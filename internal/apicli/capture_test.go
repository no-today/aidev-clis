package apicli

import (
	"strings"
	"testing"
)

func TestBodyExcerptRedactsSecrets(t *testing.T) {
	b := []byte(`{"code":1,"msg":"bad password","token":"SECRET-XYZ","data":{"accessToken":"AT-1","Authorization":"Bearer LEAK"}}`)
	out := bodyExcerpt(b)
	for _, leak := range []string{"SECRET-XYZ", "AT-1", "LEAK"} {
		if strings.Contains(out, leak) {
			t.Fatalf("excerpt leaked a secret %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "bad password") {
		t.Fatalf("excerpt should keep non-sensitive fields: %s", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("expected a redaction marker: %s", out)
	}
}

func TestBodyExcerptNonJSONFallsBack(t *testing.T) {
	out := bodyExcerpt([]byte("  plain text error  "))
	if out != "plain text error" {
		t.Fatalf("non-JSON body should trim+passthrough, got %q", out)
	}
}

func TestEvalCaptureHeaderCaseInsensitive(t *testing.T) {
	// Headers map is keyed canonically; a config using non-canonical casing must still hit.
	r := &RawResponse{Headers: map[string]string{"X-Trace-Id": "T1"}}
	if got := evalCapture("header.x-trace-id", r); got != "T1" {
		t.Fatalf("header capture should be case-insensitive, got %q", got)
	}
}

func TestEvalCapture(t *testing.T) {
	r := &RawResponse{
		Status:  201,
		Headers: map[string]string{"Access-Token": "A-1"},
		Body:    []byte(`{"data":{"token":"T-1"}}`),
	}
	cases := map[string]string{
		"body.data.token":     "T-1",
		"data.token":          "T-1", // bare == body.
		"header.Access-Token": "A-1",
		"status":              "201",
		"body.missing":        "",
	}
	for expr, want := range cases {
		if got := evalCapture(expr, r); got != want {
			t.Fatalf("evalCapture(%q) = %q, want %q", expr, got, want)
		}
	}
}

func TestMergeVars(t *testing.T) {
	out := mergeVars(
		map[string]string{"a": "1", "b": "1"},
		map[string]string{"b": "2", "c": "2"},
	)
	if out["a"] != "1" || out["b"] != "2" || out["c"] != "2" {
		t.Fatalf("later map must win: %+v", out)
	}
}
