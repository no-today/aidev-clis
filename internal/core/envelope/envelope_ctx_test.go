package envelope

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/diag"
)

func TestWriteDataCtx_NoDiag_OmitsDiagnostics(t *testing.T) {
	var b bytes.Buffer
	if err := WriteDataCtx(context.Background(), &b, []int{1}, nil, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("diagnostics must be omitted with no collector: %s", b.String())
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("data must be present: %s", b.String())
	}
}

func TestWriteDataCtx_WithLines_EmitsDiagnostics(t *testing.T) {
	d := diag.New(1)
	d.Logf(1, "resolved env=prod")
	d.Logf(1, "dialed mysql host=db.internal")
	ctx := diag.With(context.Background(), d)

	var b bytes.Buffer
	if err := WriteDataCtx(ctx, &b, []int{1}, nil, false); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Data        []int    `json:"data"`
		Diagnostics []string `json:"diagnostics"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Diagnostics) != 2 || got.Diagnostics[0] != "resolved env=prod" {
		t.Fatalf("diagnostics not merged in order: %s", b.String())
	}
}

func TestWriteDataCtx_DiagPresentButEmpty_OmitsDiagnostics(t *testing.T) {
	d := diag.New(0) // no -v: nothing recorded
	d.Logf(1, "never recorded")
	ctx := diag.With(context.Background(), d)

	var b bytes.Buffer
	_ = WriteDataCtx(ctx, &b, []int{1}, nil, false)
	var got map[string]any
	_ = json.Unmarshal(b.Bytes(), &got)
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("empty collector must omit diagnostics: %s", b.String())
	}
}

func TestWriteErrorCtx_WithLines_EmitsDiagnostics(t *testing.T) {
	d := diag.New(2)
	d.Logf(1, "resolved env=prod")
	d.Logf(2, "dial attempt host=db.internal port=3306")
	ctx := diag.With(context.Background(), d)

	var b bytes.Buffer
	if err := WriteErrorCtx(ctx, &b, "DIAL_FAILED", "connection refused", false); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Error       struct{ Code, Message string } `json:"error"`
		Diagnostics []string                       `json:"diagnostics"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "DIAL_FAILED" {
		t.Fatalf("error code lost: %s", b.String())
	}
	if len(got.Diagnostics) != 2 {
		t.Fatalf("error envelope must carry diagnostics: %s", b.String())
	}
}

func TestWriteErrorCtx_NoDiag_OmitsDiagnostics(t *testing.T) {
	var b bytes.Buffer
	_ = WriteErrorCtx(context.Background(), &b, "BAD", "nope", false)
	var got map[string]any
	_ = json.Unmarshal(b.Bytes(), &got)
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("no collector must omit diagnostics: %s", b.String())
	}
}

func lastNDJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("bad last NDJSON line %q: %v", lines[len(lines)-1], err)
	}
	return m
}

func TestStreamEndCtx_CarriesDiagnostics(t *testing.T) {
	d := diag.New(1)
	d.Logf(1, "followed 3 sources")
	ctx := diag.With(context.Background(), d)

	var b bytes.Buffer
	s := NewStream(&b)
	_ = s.Record(map[string]any{"line": "a"})
	_ = s.EndCtx(ctx, 1)

	last := lastNDJSON(t, b.String())
	if last["type"] != "end" {
		t.Fatalf("last line must be end: %s", b.String())
	}
	if last["diagnostics"] == nil {
		t.Fatalf("end line must carry diagnostics under -v: %s", b.String())
	}
}

func TestStreamEndCtx_NoDiag_OmitsField(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	_ = s.EndCtx(context.Background(), 0)
	if _, ok := lastNDJSON(t, b.String())["diagnostics"]; ok {
		t.Fatalf("end line must omit diagnostics with no collector: %s", b.String())
	}
}

func TestStreamErrCtx_CarriesDiagnostics(t *testing.T) {
	d := diag.New(2)
	d.Logf(1, "resolved env=prod")
	ctx := diag.With(context.Background(), d)

	var b bytes.Buffer
	s := NewStream(&b)
	_ = s.ErrCtx(ctx, "BACKEND_DOWN", "refused")
	last := lastNDJSON(t, b.String())
	if last["type"] != "error" || last["diagnostics"] == nil {
		t.Fatalf("error line must carry diagnostics: %s", b.String())
	}
}
