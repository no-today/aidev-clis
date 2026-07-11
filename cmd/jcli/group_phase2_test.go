package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/jcli"
)

// With two groups and no --group, jcli auto-resolves the group from the synced
// jobs cache: `build svc` finds front-end/grp/svc and builds it.
func TestBuildCmd_AutoResolvesGroup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
		case "/job/front-end/job/grp/job/svc/buildWithParameters":
			w.Header().Set("Location", "http://"+r.Host+"/queue/item/1/")
			w.WriteHeader(201)
		case "/queue/item/1/api/json":
			w.Write([]byte(`{"executable":{"number":4}}`))
		case "/job/front-end/job/grp/job/svc/4/api/json":
			if r.URL.RawQuery == "" {
				w.Write([]byte(`{"building":false,"result":"SUCCESS"}`))
			} else {
				w.Write([]byte(`{"artifacts":[]}`))
			}
		}
	}))
	defer srv.Close()

	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + srv.URL +
		"\n    credential: jenkins.t\n" +
		"    groups:\n" +
		"      frontend:\n        job_template: \"front-end/grp/${service}\"\n" +
		"      backend:\n        job_template: \"app-server/basic/${service}\"\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"tok"}`), 0o600)

	// Seed the jobs cache: svc lives under the frontend root only.
	cache := &jcli.JobsCache{Target: "t", Jobs: []jcli.CachedJob{
		{Name: "svc", Path: "front-end/grp/svc"},
		{Name: "other", Path: "app-server/basic/other"},
	}}
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}

	// No --group: must auto-resolve to frontend and build front-end/grp/svc.
	env := runCLI(t, "build", "svc", "--wait")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("build failed: %v", env)
	}
	if data["job"] != "front-end/grp/svc" {
		t.Fatalf("auto-resolved job = %v, want front-end/grp/svc", data["job"])
	}

	// A service in no group's cache → error (not a silent wrong build).
	if e := runCLI(t, "build", "ghost", "--wait"); e["error"] == nil {
		t.Fatalf("unknown service should error, got %v", e)
	}
}
