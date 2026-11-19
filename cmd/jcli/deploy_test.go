package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDeployCmd_EndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
		case "/job/build-svc/buildWithParameters":
			w.Header().Set("Location", "http://"+r.Host+"/queue/item/1/")
			w.WriteHeader(201)
		case "/queue/item/1/api/json":
			w.Write([]byte(`{"executable":{"number":4}}`))
		case "/job/build-svc/4/api/json":
			if r.URL.RawQuery == "" {
				w.Write([]byte(`{"building":false,"result":"SUCCESS"}`))
			} else {
				w.Write([]byte(`{"artifacts":[{"fileName":"a.tar.gz","relativePath":"a.tar.gz"}]}`))
			}
		case "/job/build-svc/4/consoleText":
			w.Write([]byte("img registry/uat/svc:20260628010203 pushed\n"))
		}
	}))
	defer srv.Close()

	marker := filepath.Join(home, "deployed")
	artMarker := filepath.Join(home, "artifact")
	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + srv.URL +
		"\n    credential: jenkins.t\n    groups:\n      main:\n        job_template: \"build-${service}\"\n" +
		"        deploy:\n          extract: { tag: 'registry/uat/${service}:(\\d{14})' }\n" +
		// ToSlash: a Windows backslash path breaks YAML double-quote escaping (\U ...) and bash.
		"          steps:\n            - [bash, -c, \"echo ${tag} > " + filepath.ToSlash(marker) + "\"]\n" +
		"            - [bash, -c, \"echo ${artifacts.0} > " + filepath.ToSlash(artMarker) + "\"]\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"tok"}`), 0o600)

	env := runCLI(t, "deploy", "svc")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data: %v", env)
	}
	ex, _ := data["extracted"].(map[string]any)
	if ex["tag"] != "20260628010203" {
		t.Fatalf("extracted: %v", data)
	}
	arts, ok := data["artifacts"].([]any)
	if !ok || len(arts) != 1 || arts[0].(map[string]any)["fileName"] != "a.tar.gz" {
		t.Fatalf("artifacts: %v", data)
	}
	if b, _ := os.ReadFile(marker); len(b) == 0 {
		t.Fatalf("tag step did not run")
	}
	if b, _ := os.ReadFile(artMarker); string(b) != "a.tar.gz\n" {
		t.Fatalf("artifact step output=%q", b)
	}
}
