package dbcli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestRun_Verbose_EmitsDiagnosticsOnSuccess(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")

	ctx := diag.With(context.Background(), diag.New(1))
	var buf bytes.Buffer
	code := Run(ctx, NewRegistry([]Driver{okDriver{}}), RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 0 {
		t.Fatalf("want success, code=%d out=%s", code, buf.String())
	}
	var got struct {
		Diagnostics []string `json:"diagnostics"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Diagnostics) == 0 {
		t.Fatalf("-v run must emit diagnostics: %s", buf.String())
	}
	joined := strings.Join(got.Diagnostics, "\n")
	if !strings.Contains(joined, "target=e") {
		t.Fatalf("diagnostics should mention resolved target: %s", buf.String())
	}
}

func TestRun_NoVerbose_OmitsDiagnostics(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")

	var buf bytes.Buffer // bare ctx, no collector
	code := Run(context.Background(), NewRegistry([]Driver{okDriver{}}), RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 0 {
		t.Fatalf("want success, code=%d out=%s", code, buf.String())
	}
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("no -v must omit diagnostics (contract unchanged): %s", buf.String())
	}
}

func TestRun_Verbose_ErrorEnvelopeCarriesDiagnostics(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: postgres, dsn: 'x'}\n")

	ctx := diag.With(context.Background(), diag.New(1))
	var buf bytes.Buffer
	code := Run(ctx, NewRegistry([]Driver{okDriver{}}), RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 2 || !strings.Contains(buf.String(), "ADAPTER_MISMATCH") {
		t.Fatalf("want adapter mismatch, code=%d out=%s", code, buf.String())
	}
	var got struct {
		Diagnostics []string `json:"diagnostics"`
	}
	_ = json.Unmarshal(buf.Bytes(), &got)
	if len(got.Diagnostics) == 0 {
		t.Fatalf("error envelope under -v must carry diagnostics: %s", buf.String())
	}
}
