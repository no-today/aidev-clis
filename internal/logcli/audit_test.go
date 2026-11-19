package logcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
)

// readAuditLines returns every audit JSONL line under $AIDEV_CLIS_HOME/audit/.
func readAuditLines(t *testing.T) []map[string]any {
	t.Helper()
	home, _ := config.Home()
	dir := filepath.Join(home, "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read audit dir: %v", err)
	}
	var lines []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if ln == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(ln), &m); err != nil {
				t.Fatalf("bad audit line %q: %v", ln, err)
			}
			lines = append(lines, m)
		}
	}
	return lines
}

// TestAudit_BatchSuccessWritesOneLineWithLines asserts that a successful batch
// fetch writes exactly one terminal line: outcome=ok, result.lines=K, no id,
// and command contains "logcli".
func TestAudit_BatchSuccessWritesOneLineWithLines(t *testing.T) {
	writeCfg(t) // sets AIDEV_CLIS_HOME to its own temp dir

	const K = 3
	records := make([]any, K)
	for i := range records {
		records[i] = map[string]any{"msg": "line"}
	}
	stub := &stubAdapter{name: "stub", records: records}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}

	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]

	if ln["outcome"] != "ok" {
		t.Fatalf("outcome = %v, want ok", ln["outcome"])
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("logcli must have no id (read-only): %v", ln)
	}
	cmd, _ := ln["command"].(string)
	if !strings.Contains(cmd, "logcli") {
		t.Fatalf("command = %q, want it to contain logcli", cmd)
	}
	res, ok := ln["result"].(map[string]any)
	if !ok {
		t.Fatalf("want result field, got none: %v", ln)
	}
	got, ok := res["lines"].(float64)
	if !ok {
		t.Fatalf("result.lines = %v (%T), want a number", res["lines"], res["lines"])
	}
	if got != float64(K) {
		t.Fatalf("result.lines = %v, want %d", got, K)
	}
}

// panicAdapter panics mid-dispatch before writing output.
type panicAdapter struct{ name string }

func (p *panicAdapter) Name() string { return p.name }
func (p *panicAdapter) Run(context.Context, Input, Output) error {
	panic("runtime error: index out of range [2]")
}

// An adapter panic must be recovered into a normal {error} envelope with an
// ADAPTER_PANIC code, and the audit terminal line must still be written (not the
// deferred op.Finish's nil-error line, and not a crash).
func TestRun_AdapterPanicYieldsErrorEnvelopeAndFinish(t *testing.T) {
	writeCfg(t)
	reg := NewRegistry([]Adapter{&panicAdapter{name: "stub"}})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out})
	if code == 0 {
		t.Fatalf("want non-zero exit on panic, out=%s", out.String())
	}
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("panic must yield a JSON envelope, not a stacktrace: %s", out.String())
	}
	ee, ok := env["error"].(map[string]any)
	if !ok || ee["code"] != "ADAPTER_PANIC" {
		t.Fatalf("want error.code=ADAPTER_PANIC, got %v", env)
	}
	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line (read-only), got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "error" || ln["code"] != "ADAPTER_PANIC" {
		t.Fatalf("terminal line must be error/ADAPTER_PANIC (Finish ran), got %v", ln)
	}
}

// TestAudit_UnknownAdapterWritesOneErrorLine asserts that an unknown-adapter
// error writes exactly one terminal line: outcome=error, a code field, no id.
func TestAudit_UnknownAdapterWritesOneErrorLine(t *testing.T) {
	writeCfg(t) // sets AIDEV_CLIS_HOME to its own temp dir

	reg := NewRegistry(nil) // no adapters registered
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "ghost", Out: &out})
	if code == 0 {
		t.Fatalf("want non-zero exit for unknown adapter")
	}

	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]

	if ln["outcome"] != "error" {
		t.Fatalf("outcome = %v, want error", ln["outcome"])
	}
	if ln["code"] == "" || ln["code"] == nil {
		t.Fatalf("want a code field on error, got none: %v", ln)
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("logcli must have no id (read-only): %v", ln)
	}
}
