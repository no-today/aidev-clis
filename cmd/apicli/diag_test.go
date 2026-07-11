package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCall_Verbose_EmitsDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":7}}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	out := runCLI(t, "call", "shop", "/api/x", "-v")
	var env struct {
		Diagnostics []string `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("not JSON: %s", out)
	}
	joined := strings.Join(env.Diagnostics, "\n")
	if !strings.Contains(joined, "app=shop") {
		t.Fatalf("want resolved app in diagnostics: %s", out)
	}
	if !strings.Contains(joined, "status=200") {
		t.Fatalf("want HTTP status in diagnostics: %s", out)
	}
}

func TestCall_NoVerbose_OmitsDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	out := runCLI(t, "call", "shop", "/api/x")
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if _, ok := got["diagnostics"]; ok {
		t.Fatalf("no -v must omit diagnostics: %s", out)
	}
}
