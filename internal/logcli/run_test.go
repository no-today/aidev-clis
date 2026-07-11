package logcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubAdapter routes by Args[0]: "stream" -> out.Stream, else -> out.Batch.
type stubAdapter struct {
	name    string
	records []any
	gotEnv  string
	gotArgs []string
}

func (s *stubAdapter) Name() string { return s.name }
func (s *stubAdapter) Run(_ context.Context, in Input, out Output) error {
	s.gotEnv = in.Target.Name
	s.gotArgs = in.Args
	if len(in.Args) > 0 && in.Args[0] == "stream" {
		st := out.Stream()
		for _, r := range s.records {
			if err := st.Record(r); err != nil {
				return err
			}
		}
		return nil
	}
	return out.Batch(s.records)
}

func writeCfg(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	_ = os.WriteFile(filepath.Join(home, "logcli.yaml"),
		[]byte("default_target: app\ntargets: {app: {adapter: stub, path: /x}}"), 0o644)
}

func TestRun_BatchPath(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}, map[string]any{"a": 2}}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	if stub.gotEnv != "app" || len(stub.gotArgs) != 1 {
		t.Fatalf("env/args not passed: %q %v", stub.gotEnv, stub.gotArgs)
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("not a batch {data} envelope: %s", out.String())
	}
	if len(env.Data) != 2 {
		t.Fatalf("want 2 rows, got %d", len(env.Data))
	}
}

func TestRun_StreamPath(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"l": "a"}, map[string]any{"l": "b"}}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"stream"}, Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	lines := bytes.Count(bytes.TrimSpace(out.Bytes()), []byte("\n")) + 1
	if lines != 3 {
		t.Fatalf("want 3 ndjson lines, got %d: %s", lines, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"type":"end"`)) {
		t.Fatalf("missing end line: %s", out.String())
	}
}

func TestRun_UnknownAdapter(t *testing.T) {
	writeCfg(t)
	reg := NewRegistry(nil)
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "ghost", Out: &out})
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"error"`)) {
		t.Fatalf("want error envelope: %s", out.String())
	}
}

func TestRunOutput_DoubleOutputRejected(t *testing.T) {
	o := &runOutput{w: &bytes.Buffer{}}
	if err := o.Batch(nil); err != nil {
		t.Fatalf("first Batch: %v", err)
	}
	if err := o.Batch(nil); err == nil {
		t.Fatal("second Batch must be rejected")
	}
	o2 := &runOutput{w: &bytes.Buffer{}}
	if err := o2.Record(nil); err == nil {
		t.Fatal("Record without Stream must be rejected")
	}
}

func TestRun_RawBatchIsVerbatimLines(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{"line one", "line two"}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out, Raw: true})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	if out.String() != "line one\nline two\n" {
		t.Fatalf("raw batch = %q", out.String())
	}
}

func TestRun_RawStreamHasNoNDJSON(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{"a", "b"}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"stream"}, Out: &out, Raw: true})
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if bytes.Contains(out.Bytes(), []byte(`"type"`)) {
		t.Fatalf("raw stream must not be NDJSON: %s", out.String())
	}
	if out.String() != "a\nb\n" {
		t.Fatalf("raw stream = %q", out.String())
	}
}

func TestRun_RawUnknownAdapterIsPlainError(t *testing.T) {
	writeCfg(t)
	reg := NewRegistry(nil)
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "ghost", Out: &out, Raw: true})
	if code == 0 {
		t.Fatalf("want non-zero exit")
	}
	got := out.String()
	if !strings.HasPrefix(got, "ERROR ") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("raw error = %q, want ERROR <code>: <msg> line", got)
	}
	if strings.Contains(got, `"error"`) || strings.Contains(got, "{") {
		t.Fatalf("raw error must not be an envelope: %q", got)
	}
}

// --fields projects each map record down to the named fields; missing fields
// are absent, non-map records pass through.
func TestRun_FieldsProjection(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{
		map[string]any{"message": "boom", "level": "ERROR", "__tag__:x": "noise"},
		"plain line",
	}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Fields: "message, level", Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	var env struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil || len(env.Data) != 2 {
		t.Fatalf("envelope: %s", out.String())
	}
	m := env.Data[0].(map[string]any)
	if m["message"] != "boom" || m["level"] != "ERROR" {
		t.Fatalf("projected fields missing: %v", m)
	}
	if _, noisy := m["__tag__:x"]; noisy {
		t.Fatalf("projection must drop unlisted fields: %v", m)
	}
	if env.Data[1] != "plain line" {
		t.Fatalf("non-map record must pass through: %v", env.Data[1])
	}
}

// With >1 configured target and no explicit --target, the envelope warns which
// target actually served the query; an explicit --target stays warning-free.
func TestRun_DefaultTargetWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	_ = os.WriteFile(filepath.Join(home, "logcli.yaml"),
		[]byte("default_target: app\ntargets: {app: {adapter: stub, path: /x}, other: {adapter: stub, path: /y}}"), 0o644)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}}}
	reg := NewRegistry([]Adapter{stub})

	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `used default target \"app\"`) {
		t.Fatalf("implicit default with 2 targets must warn: %s", out.String())
	}

	out.Reset()
	code = Run(context.Background(), reg, RunArgs{Adapter: "stub", Target: "app", Args: []string{"logs"}, Out: &out})
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	if strings.Contains(out.String(), "warnings") {
		t.Fatalf("explicit --target must not warn: %s", out.String())
	}
}

// A single-target config keeps the implicit default warning-free (nothing to
// confuse it with).
func TestRun_SingleTargetNoWarning(t *testing.T) {
	writeCfg(t)
	stub := &stubAdapter{name: "stub", records: []any{map[string]any{"a": 1}}}
	reg := NewRegistry([]Adapter{stub})
	var out bytes.Buffer
	code := Run(context.Background(), reg, RunArgs{Adapter: "stub", Args: []string{"logs"}, Out: &out})
	if code != 0 || strings.Contains(out.String(), "warnings") {
		t.Fatalf("single-target default must not warn: exit=%d %s", code, out.String())
	}
}
