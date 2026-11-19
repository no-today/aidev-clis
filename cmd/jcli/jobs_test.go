package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestJobsCmd_SyncThenRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/json" {
			w.Write([]byte(`{"jobs":[{"name":"back","url":"u","jobs":[{"name":"svc-a","url":"u2"}]}]}`))
		}
	}))
	defer srv.Close()
	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + srv.URL +
		"\n    credential: jenkins.t\n    groups:\n      main: {}\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"t"}`), 0o600)

	// --sync writes the cache
	env := runCLI(t, "jobs", "--sync")
	data := env["data"].(map[string]any)
	jobs := data["jobs"].([]any)
	if len(jobs) != 1 || jobs[0].(map[string]any)["path"] != "back/svc-a" {
		t.Fatalf("sync: %v", data)
	}
	// plain `jobs` reads it back (no server hit needed)
	env2 := runCLI(t, "jobs")
	if len(env2["data"].(map[string]any)["jobs"].([]any)) != 1 {
		t.Fatalf("read: %v", env2)
	}
	if _, err := os.Stat(filepath.Join(home, "cache", "jcli", "t", "jobs.json")); err != nil {
		t.Fatalf("cache file: %v", err)
	}
}
