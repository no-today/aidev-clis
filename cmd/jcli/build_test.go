package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestBuildCmd_Artifacts(t *testing.T) {
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
			if r.URL.RawQuery == "" { // Wait() polls without a query
				w.Write([]byte(`{"building":false,"result":"SUCCESS"}`))
			} else { // Artifacts() asks for ?tree=artifacts[...]
				w.Write([]byte(`{"artifacts":[{"fileName":"a.tar.gz","relativePath":"a.tar.gz"}]}`))
			}
		}
	}))
	defer srv.Close()

	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + srv.URL +
		"\n    credential: jenkins.t\n    groups:\n      main:\n        job_template: \"build-${service}\"\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), []byte(`{"user":"ci","api_token":"tok"}`), 0o600)

	env := runCLI(t, "build", "svc", "--wait")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data: %v", env)
	}
	arts, ok := data["artifacts"].([]any)
	if !ok || len(arts) != 1 {
		t.Fatalf("artifacts: %v", data)
	}
	if arts[0].(map[string]any)["fileName"] != "a.tar.gz" {
		t.Fatalf("fileName: %v", arts[0])
	}
}

// TestBuildCmd_WaitTimeout proves a hung build can't block forever: with --wait
// and a tiny --timeout, the deadline surfaces as a clean Timeout (exit 4, NOT a
// remote failure). It also locks the partial-result fix — even though Wait never
// reported a build number, the audit terminal line keeps the triggered one (9),
// not a clobbered 0.
func TestBuildCmd_WaitTimeout(t *testing.T) {
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
			w.Write([]byte(`{"executable":{"number":9}}`))
		case "/job/build-svc/9/api/json":
			// The build never finishes — Wait polls this forever until the deadline.
			w.Write([]byte(`{"building":true}`))
		}
	}))
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	root := newRoot()
	root.SetArgs([]string{"build", "svc", "--wait", "--timeout", "50ms"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	e := errs.From(err)
	if e.Exit != errs.ExitTimeout {
		t.Fatalf("exit = %d (%s), want %d (timeout)", e.Exit, e.Code, errs.ExitTimeout)
	}

	lines := readJcliAuditLines(t, home)
	terminal := lines[len(lines)-1]
	if terminal["outcome"] != "error" {
		t.Fatalf("terminal outcome = %v, want error", terminal["outcome"])
	}
	res, ok := terminal["result"].(map[string]any)
	if !ok {
		t.Fatalf("timed-out terminal must still carry a result: %v", terminal)
	}
	if res["build"].(float64) != 9 {
		t.Fatalf("result.build = %v, want 9 (triggered number preserved through a failed Wait)", res["build"])
	}
}
