package dbcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// writeDriver is a stub that emits the write payload convention: {affected:N}.
type writeDriver struct{ affected int }

func (writeDriver) Name() string { return "mysql" }
func (d writeDriver) Run(_ context.Context, _ Input, out Output) error {
	return out.Batch(map[string]any{"affected": d.affected})
}

// mapNoAffectedDriver emits a map payload WITHOUT an "affected" key (e.g. a
// ping/describe-style object) — it must NOT produce an audit result summary.
type mapNoAffectedDriver struct{}

func (mapNoAffectedDriver) Name() string { return "mysql" }
func (mapNoAffectedDriver) Run(_ context.Context, _ Input, out Output) error {
	return out.Batch(map[string]any{"ok": true})
}

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

func TestAudit_ReadSingleTerminalLineWithRows(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{okDriver{}}),
		RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, buf.String())
	}
	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("read must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "ok" {
		t.Fatalf("outcome = %v, want ok", ln["outcome"])
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("read must have no id: %v", ln)
	}
	cmd, _ := ln["command"].(string)
	if !strings.Contains(cmd, "dbcli") {
		t.Fatalf("command = %q, want it to contain dbcli", cmd)
	}
	res, ok := ln["result"].(map[string]any)
	if !ok {
		t.Fatalf("read must have a result: %v", ln)
	}
	if res["rows"].(float64) != 1 {
		t.Fatalf("result.rows = %v, want 1", res["rows"])
	}
}

func TestAudit_WriteTwoPhaseSharesIDWithAffected(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{writeDriver{affected: 3}}),
		RunArgs{Driver: "mysql", Target: "e", AllowWrite: true, Out: &buf})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, buf.String())
	}
	lines := readAuditLines(t)
	if len(lines) != 2 {
		t.Fatalf("write must audit two lines, got %d: %v", len(lines), lines)
	}
	started, terminal := lines[0], lines[1]
	if started["outcome"] != "started" {
		t.Fatalf("first line outcome = %v, want started", started["outcome"])
	}
	if terminal["outcome"] != "ok" {
		t.Fatalf("terminal outcome = %v, want ok", terminal["outcome"])
	}
	id1, _ := started["id"].(string)
	id2, _ := terminal["id"].(string)
	if id1 == "" || id1 != id2 {
		t.Fatalf("started and terminal must share a non-empty id: %q vs %q", id1, id2)
	}
	if _, ok := started["result"]; ok {
		t.Fatalf("started line must have no result: %v", started)
	}
	res, ok := terminal["result"].(map[string]any)
	if !ok {
		t.Fatalf("write terminal must have a result: %v", terminal)
	}
	if res["affected"].(float64) != 3 {
		t.Fatalf("result.affected = %v, want 3", res["affected"])
	}
}

func TestAudit_MapPayloadWithoutAffectedHasNoResult(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{mapNoAffectedDriver{}}),
		RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, buf.String())
	}
	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "ok" {
		t.Fatalf("outcome = %v, want ok", ln["outcome"])
	}
	if _, ok := ln["result"]; ok {
		t.Fatalf("a map payload without an affected key must have no result: %v", ln)
	}
}

// panicDriver panics mid-dispatch (e.g. a bad arg-index) BEFORE writing output.
type panicDriver struct{}

func (panicDriver) Name() string { return "mysql" }
func (panicDriver) Run(context.Context, Input, Output) error {
	panic("runtime error: index out of range [3]")
}

// A driver panic must be recovered into a normal {error} envelope with a
// DRIVER_PANIC code and exit 1, AND the audit lifecycle must still terminate
// (started + terminal error line) — no dangling "started" line.
func TestRun_DriverPanicYieldsErrorEnvelopeAndFinish(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	// AllowWrite:true → side-effecting → a started line is written before dispatch,
	// so this also proves the panic path still writes the terminal line.
	code := Run(context.Background(), NewRegistry([]Driver{panicDriver{}}),
		RunArgs{Driver: "mysql", Target: "e", AllowWrite: true, Out: &buf})
	if code != errs.ExitGeneral {
		t.Fatalf("exit=%d want %d; out=%s", code, errs.ExitGeneral, buf.String())
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("panic must yield a JSON envelope, not a stacktrace: %s", buf.String())
	}
	ee, ok := env["error"].(map[string]any)
	if !ok || ee["code"] != "DRIVER_PANIC" {
		t.Fatalf("want error.code=DRIVER_PANIC, got %v", env)
	}
	lines := readAuditLines(t)
	if len(lines) != 2 {
		t.Fatalf("panic must still audit started+terminal (2 lines), got %d: %v", len(lines), lines)
	}
	started, terminal := lines[0], lines[1]
	if started["outcome"] != "started" {
		t.Fatalf("first line outcome = %v, want started", started["outcome"])
	}
	if terminal["outcome"] != "error" || terminal["code"] != "DRIVER_PANIC" {
		t.Fatalf("terminal line must be error/DRIVER_PANIC (Finish ran), got %v", terminal)
	}
	id1, _ := started["id"].(string)
	id2, _ := terminal["id"].(string)
	if id1 == "" || id1 != id2 {
		t.Fatalf("started and terminal must share a non-empty id: %q vs %q", id1, id2)
	}
}

func TestAudit_DoctorFailRecordsRealStageCode(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x', ssh: {host: h, user: u, identity_file: k}}\n")
	var buf bytes.Buffer
	d := errDriver{err: errs.Remote("SSH_TCP_DIAL", "dial bastion failed")}
	code := Run(context.Background(), NewRegistry([]Driver{d}),
		RunArgs{Driver: "mysql", Target: "e", Args: []string{"doctor"}, Out: &buf})
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("doctor must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "error" {
		t.Fatalf("outcome = %v, want error", ln["outcome"])
	}
	// Audit fidelity: the real classified stage code, not synthetic DOCTOR_UNHEALTHY.
	if ln["code"] != "SSH_TCP_DIAL" {
		t.Fatalf("code = %v, want SSH_TCP_DIAL (real stage code)", ln["code"])
	}
}

func TestAudit_ErrorPathHasCode(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry(nil), RunArgs{Driver: "nope", Out: &buf})
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	lines := readAuditLines(t)
	if len(lines) != 1 {
		t.Fatalf("error must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "error" {
		t.Fatalf("outcome = %v, want error", ln["outcome"])
	}
	if ln["code"] != "UNKNOWN_DRIVER" {
		t.Fatalf("code = %v, want UNKNOWN_DRIVER", ln["code"])
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("early error must have no id: %v", ln)
	}
}
