package logcli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestRun_Verbose_BatchEmitsDiagnostics(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}, map[string]any{"a": 2}}}
	reg := NewRegistry([]Adapter{stub})

	d := diag.New(1)
	ctx := diag.With(context.Background(), d)
	var out bytes.Buffer
	if code := Run(ctx, reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out}); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	var env struct {
		Diagnostics []string `json:"diagnostics"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env.Diagnostics, "\n")
	if !strings.Contains(joined, "target=app") {
		t.Fatalf("want resolved target in diagnostics: %s", out.String())
	}
	if !strings.Contains(joined, "2 record") {
		t.Fatalf("want record count in diagnostics: %s", out.String())
	}
}

func TestRun_NoVerbose_BatchOmitsDiagnostics(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	if code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out}); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	var got map[string]any
	_ = json.Unmarshal(out.Bytes(), &got)
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("no -v must omit diagnostics: %s", out.String())
	}
}

func TestRun_Verbose_StreamEndCarriesDiagnostics(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}, map[string]any{"a": 2}}}
	reg := NewRegistry([]Adapter{stub})

	d := diag.New(1)
	ctx := diag.With(context.Background(), d)
	var out bytes.Buffer
	if code := Run(ctx, reg, RunArgs{Adapter: "stub", Args: []string{"stream"}, Out: &out}); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last["type"] != "end" {
		t.Fatalf("last line must be end: %s", out.String())
	}
	if last["diagnostics"] == nil {
		t.Fatalf("stream end must carry diagnostics under -v: %s", out.String())
	}
}
