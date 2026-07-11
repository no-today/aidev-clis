package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeDoctorEnv(t *testing.T, home, baseURL string) {
	t.Helper()
	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + baseURL +
		"\n    credential: jenkins.t\n    groups:\n      main: {}\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"t"}`), 0o600)
}

func TestDoctorCmd_OK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	doctorExit = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me/api/json" {
			w.Write([]byte(`{"id":"ci","fullName":"CI Bot"}`))
		}
	}))
	defer srv.Close()
	writeDoctorEnv(t, home, srv.URL)

	env := runCLI(t, "doctor")
	checks, ok := env["data"].([]any)
	if !ok || len(checks) != 2 {
		t.Fatalf("doctor data: %v", env)
	}
	auth := checks[1].(map[string]any)
	if auth["name"] != "auth" || auth["ok"] != true {
		t.Fatalf("auth check: %v", auth)
	}
	if doctorExit != 0 {
		t.Fatalf("healthy doctor exit = %d", doctorExit)
	}
}

func TestDoctorCmd_AuthFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	doctorExit = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden) // every request → 403
	}))
	defer srv.Close()
	writeDoctorEnv(t, home, srv.URL)

	env := runCLI(t, "doctor")
	checks := env["data"].([]any)
	// config ok, auth failed
	if len(checks) != 2 || checks[1].(map[string]any)["ok"] != false {
		t.Fatalf("expected auth failure: %v", checks)
	}
	if doctorExit == 0 {
		t.Fatal("failed doctor must set a non-zero exit")
	}
}
