package dbcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
)

type okDriver struct{}

func (okDriver) Name() string { return "mysql" }
func (okDriver) Run(_ context.Context, _ Input, out Output) error {
	return out.Batch(Result{Columns: []string{"id"}, Rows: [][]any{{int64(1)}}})
}

type silentDriver struct{}

func (silentDriver) Name() string                             { return "mysql" }
func (silentDriver) Run(context.Context, Input, Output) error { return nil }

func TestRun_UnknownDriver(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir()) // Run audits even pre-dispatch rejections
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry(nil), RunArgs{Driver: "nope", Out: &buf})
	if code != 2 { // ExitConfig
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(buf.String(), "UNKNOWN_DRIVER") {
		t.Fatalf("envelope: %s", buf.String())
	}
}

func TestRun_AdapterMismatch(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: postgres, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{okDriver{}}), RunArgs{Driver: "mysql", Target: "e", Out: &buf})
	if code != 2 || !strings.Contains(buf.String(), "ADAPTER_MISMATCH") {
		t.Fatalf("code=%d env=%s", code, buf.String())
	}
}

func writeDBConfig(t *testing.T, body string) {
	t.Helper()
	home, _ := config.Home()
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunOutput_RawWritesVerbatimAndMarksWritten(t *testing.T) {
	var buf bytes.Buffer
	o := &runOutput{w: &buf}
	if err := o.Raw("INSERT INTO `t` (`id`) VALUES (1);\n"); err != nil {
		t.Fatal(err)
	}
	if !o.wrote {
		t.Fatal("Raw must set wrote=true")
	}
	if buf.String() != "INSERT INTO `t` (`id`) VALUES (1);\n" {
		t.Fatalf("verbatim: %q", buf.String())
	}
	// runOutput must satisfy the optional RawOutput interface.
	var _ RawOutput = o
}

func TestRun_RawBatchIsTSV(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{okDriver{}}),
		RunArgs{Driver: "mysql", Target: "e", Out: &buf, Raw: true})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, buf.String())
	}
	if buf.String() != "1\n" { // one row, one cell, no header, no envelope
		t.Fatalf("raw batch = %q, want %q", buf.String(), "1\n")
	}
}

func TestRun_RawErrorIsPlainLine(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir()) // Run audits even pre-dispatch rejections
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry(nil), RunArgs{Driver: "nope", Out: &buf, Raw: true})
	if code != 2 {
		t.Fatalf("exit=%d", code)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "ERROR UNKNOWN_DRIVER: ") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("raw error = %q, want ERROR UNKNOWN_DRIVER: <msg> line", got)
	}
	if strings.Contains(got, "{") {
		t.Fatalf("raw error must not be an envelope: %q", got)
	}
}

func TestRun_RawProducedNothingIsEmpty(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeDBConfig(t, "default_target: e\ntargets:\n  e: {adapter: mysql, dsn: 'x'}\n")
	var buf bytes.Buffer
	code := Run(context.Background(), NewRegistry([]Driver{silentDriver{}}), RunArgs{Driver: "mysql", Target: "e", Out: &buf, Raw: true})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, buf.String())
	}
	if buf.String() != "" {
		t.Fatalf("raw produced-nothing should be empty, got %q", buf.String())
	}
}

// Removed v1 subcommands and `version` land in the driver slot; the error must
// name the migration instead of the baffling "no db driver query".
func TestUnknownDriverMsg(t *testing.T) {
	reg := NewRegistry([]Driver{okDriver{}})
	if got := unknownDriverMsg(reg, "query"); !strings.Contains(got, "driver-first") || !strings.Contains(got, "mysql") {
		t.Fatalf("removed-subcommand hint missing: %q", got)
	}
	if got := unknownDriverMsg(reg, "version"); !strings.Contains(got, "--version") {
		t.Fatalf("version hint missing: %q", got)
	}
	if got := unknownDriverMsg(reg, "nope"); !strings.Contains(got, `no db driver "nope"`) || !strings.Contains(got, "mysql") {
		t.Fatalf("generic unknown-driver must list drivers: %q", got)
	}
}
