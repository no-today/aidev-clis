package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func twoGroupCfg(home, baseURL string) string {
	return "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + baseURL +
		"\n    credential: jenkins.t\n" +
		"    groups:\n" +
		"      frontend:\n        job_template: \"front-end/grp/${service}\"\n" +
		// ToSlash: a Windows backslash path breaks YAML double-quote escaping (\U ...) and bash.
		"        deploy:\n          steps:\n            - [bash, -c, \"echo fe > " + filepath.ToSlash(filepath.Join(home, "fe-ran")) + "\"]\n" +
		"      backend:\n        job_template: \"app-server/basic/${service}\"\n"
}

func writeTwoGroupEnv(t *testing.T, home, baseURL string) {
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(twoGroupCfg(home, baseURL)), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"tok"}`), 0o600)
}

// build with --job on a multi-group env needs no --group (build ignores the group).
func TestJobBypass_BuildNoGroup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
		case "/job/app-server/job/basic/job/thing/buildWithParameters":
			w.Header().Set("Location", "http://"+r.Host+"/queue/item/1/")
			w.WriteHeader(201)
		case "/queue/item/1/api/json":
			w.Write([]byte(`{"executable":{"number":4}}`))
		case "/job/app-server/job/basic/job/thing/4/api/json":
			if r.URL.RawQuery == "" {
				w.Write([]byte(`{"building":false,"result":"SUCCESS"}`))
			} else {
				w.Write([]byte(`{"artifacts":[]}`))
			}
		}
	}))
	defer srv.Close()
	writeTwoGroupEnv(t, home, srv.URL)

	env := runCLI(t, "build", "--job", "app-server/basic/thing", "--wait")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("build --job should not require --group: %v", env)
	}
	if data["job"] != "app-server/basic/thing" {
		t.Fatalf("job = %v, want app-server/basic/thing", data["job"])
	}
}

// deploy with --job on a multi-group env infers the group from the job path and
// runs that group's deploy flow; a path under no group still errors.
func TestDeployJob_InfersGroup(t *testing.T) {
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
		case "/job/front-end/job/grp/job/svc/4/consoleText":
			w.Write([]byte("ok\n"))
		}
	}))
	defer srv.Close()
	writeTwoGroupEnv(t, home, srv.URL)

	// --job front-end/grp/svc → inferred as frontend → frontend's deploy step runs.
	env := runCLI(t, "deploy", "--job", "front-end/grp/svc")
	if env["data"] == nil {
		t.Fatalf("deploy --job should infer frontend: %v", env)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "fe-ran")); string(b) != "fe\n" {
		t.Fatalf("inferred frontend step did not run: %q", b)
	}

	// A path under no group → deploy still errors (needs --group).
	if e := runCLI(t, "deploy", "--job", "nowhere/thing"); e["error"] == nil {
		t.Fatalf("deploy --job with an ungrouped path should error, got %v", e)
	}
}
