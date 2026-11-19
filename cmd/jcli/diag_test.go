package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func buildSrv(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
		case "/job/build-svc/buildWithParameters":
			w.Header().Set("Location", "http://"+r.Host+"/queue/item/1/")
			w.WriteHeader(201)
		case "/queue/item/1/api/json":
			w.Write([]byte(`{"executable":{"number":3}}`))
		case "/job/build-svc/3/api/json":
			w.Write([]byte(`{"building":false,"result":"SUCCESS","number":3}`))
		}
	}))
}

func TestBuildCmd_Verbose_EmitsDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := buildSrv(t)
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	env := runCLI(t, "build", "svc", "--wait", "-v")
	diags, ok := env["diagnostics"].([]any)
	if !ok || len(diags) == 0 {
		t.Fatalf("want diagnostics under -v: %v", env)
	}
	if joined := fmt.Sprintf("%v", diags); !strings.Contains(joined, "job=build-svc") {
		t.Fatalf("diagnostics should mention resolved job: %v", diags)
	}
}

func TestBuildCmd_NoVerbose_OmitsDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := buildSrv(t)
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	env := runCLI(t, "build", "svc", "--wait")
	if _, ok := env["diagnostics"]; ok {
		t.Fatalf("no -v must omit diagnostics: %v", env)
	}
}
