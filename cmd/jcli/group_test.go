package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A two-group env: --group selects which group's job_template + deploy flow runs,
// and an omitted --group with multiple groups errors GROUP_REQUIRED.
func TestDeployCmd_GroupSelectsFlow(t *testing.T) {
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

	marker := filepath.Join(home, "fe-ran")
	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + srv.URL +
		"\n    credential: jenkins.t\n" +
		"    groups:\n" +
		"      frontend:\n" +
		"        job_template: \"front-end/grp/${service}\"\n" +
		// ToSlash: a Windows backslash path breaks YAML double-quote escaping (\U ...) and bash.
		"        deploy:\n          steps:\n            - [bash, -c, \"echo fe > " + filepath.ToSlash(marker) + "\"]\n" +
		"      backend:\n" +
		"        job_template: \"app-server/basic/${service}\"\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"tok"}`), 0o600)

	// No --group with two groups → must error GROUP_REQUIRED.
	if e, _ := runCLI(t, "deploy", "svc")["error"].(map[string]any); e == nil || e["code"] != "GROUP_REQUIRED" {
		t.Fatalf("want GROUP_REQUIRED error, got %v", e)
	}

	// --group frontend → resolves front-end/grp/svc and runs the frontend step.
	env := runCLI(t, "deploy", "svc", "--group", "frontend")
	if env["data"] == nil {
		t.Fatalf("deploy failed: %v", env)
	}
	if b, _ := os.ReadFile(marker); string(b) != "fe\n" {
		t.Fatalf("frontend step did not run: %q", b)
	}
}
