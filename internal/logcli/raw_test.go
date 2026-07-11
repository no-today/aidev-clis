package logcli

import "testing"

func TestFormatRawRecord_String(t *testing.T) {
	if got := formatRawRecord("hello world"); got != "hello world" {
		t.Fatalf("string record = %q", got)
	}
}

func TestFormatRawRecord_PromotedFields(t *testing.T) {
	rec := map[string]any{
		"time": "2026-06-29T10:00:01", "level": "ERROR",
		"service": "api", "trace_id": "abc", "message": "boom",
	}
	want := "2026-06-29T10:00:01 ERROR [api] trace=abc boom"
	if got := formatRawRecord(rec); got != want {
		t.Fatalf("promoted = %q, want %q", got, want)
	}
}

func TestFormatRawRecord_UnknownMapIsJSON(t *testing.T) {
	if got := formatRawRecord(map[string]any{"name": "pod-1"}); got != `{"name":"pod-1"}` {
		t.Fatalf("unknown map = %q", got)
	}
}

func TestFormatRawRecord_MessageOnlyIsBareLine(t *testing.T) {
	// the line-adapter shape: {"message": line} -> verbatim line, no decoration
	if got := formatRawRecord(map[string]any{"message": "plain log line"}); got != "plain log line" {
		t.Fatalf("message-only = %q, want %q", got, "plain log line")
	}
}

func TestFormatRawRecord_EmptyMapIsJSON(t *testing.T) {
	if got := formatRawRecord(map[string]any{}); got != "{}" {
		t.Fatalf("empty map = %q, want %q", got, "{}")
	}
}

func TestFormatRawRecord_SLSStyleRecordFallsBackToJSON(t *testing.T) {
	// SLS-style keys are not in the promoted set -> compact JSON (keys sorted by json.Marshal)
	got := formatRawRecord(map[string]any{"__time__": "1718000000", "msg": "boom"})
	want := `{"__time__":"1718000000","msg":"boom"}`
	if got != want {
		t.Fatalf("sls-style = %q, want %q", got, want)
	}
}

func TestFormatRawRecord_NonMapDefault(t *testing.T) {
	if got := formatRawRecord([]any{"a", "b"}); got != `["a","b"]` {
		t.Fatalf("slice default = %q, want %q", got, `["a","b"]`)
	}
}
