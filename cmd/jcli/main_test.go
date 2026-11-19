package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

func writeJcliEnv(t *testing.T, home, baseURL string) {
	t.Helper()
	cfg := "default_target: t\ntargets:\n  t:\n    adapter: jenkins\n    base_url: " + baseURL +
		"\n    credential: jenkins.t\n    groups:\n      main:\n        job_template: \"build-${service}\"\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	b, _ := json.Marshal(map[string]string{"user": "ci", "api_token": "tok"})
	_ = os.WriteFile(filepath.Join(dir, "jenkins.t"), b, 0o600)
}

func runCLI(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out := runCLIRaw(t, args...)
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout not JSON: %q", out)
	}
	return env
}

// runCLIRaw runs the CLI and returns stdout verbatim — for commands like
// `log --output raw` that bypass the JSON envelope.
func runCLIRaw(t *testing.T, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	root := newRoot()
	root.SetArgs(args)
	if runErr := root.Execute(); runErr != nil {
		e := errs.From(runErr)
		_ = envelope.WriteError(os.Stdout, e.Code, e.Message, false)
	}
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestTargetsCmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeJcliEnv(t, home, "http://x")
	result := runCLI(t, "targets")
	arr, ok := result["data"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("targets data: %v", result)
	}
}

func TestConfigDirFlagSetsHome(t *testing.T) {
	// t.Setenv records the original so Go restores it after the test, even though
	// the CLI's PersistentPreRunE calls os.Setenv("AIDEV_CLIS_HOME", dir) itself.
	t.Setenv("AIDEV_CLIS_HOME", "")
	dir := t.TempDir() // empty: no jcli.yaml
	out := runCLIRaw(t, "--config-dir", dir, "targets")
	// Unmarshal before asserting on the path: in the raw JSON a Windows dir
	// (C:\Users\...) is backslash-escaped, so strings.Contains(out, dir) can
	// never match there (see docs/CROSS-PLATFORM.md).
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not an error envelope: %s", out)
	}
	if env.Error.Code != "CONFIG_MISSING" || !strings.Contains(env.Error.Message, dir) {
		t.Fatalf("expected CONFIG_MISSING pointing at --config-dir %s, got: %s", dir, out)
	}
}

func TestBuildCmd_EndToEnd(t *testing.T) {
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
			w.Write([]byte(`{"executable":{"number":3}}`))
		case "/job/build-svc/3/api/json":
			w.Write([]byte(`{"building":false,"result":"SUCCESS","number":3}`))
		}
	}))
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	env := runCLI(t, "build", "svc", "--wait")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data envelope: %v", env)
	}
	if data["result"] != "SUCCESS" || data["build"].(float64) != 3 {
		t.Fatalf("bad build result: %v", data)
	}
}
