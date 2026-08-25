package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deployServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
		case "/job/build-svc/buildWithParameters":
			w.Header().Set("Location", "http://"+r.Host+"/queue/item/1/")
			w.WriteHeader(201)
		case "/queue/item/1/api/json":
			w.Write([]byte(`{"executable":{"number":7}}`))
		case "/job/build-svc/7/api/json":
			w.Write([]byte(`{"building":false,"result":"SUCCESS"}`))
		case "/job/build-svc/7/consoleText":
			w.Write([]byte("log...\nPushed registry/uat/svc:20260628120000\ndone\n"))
		}
	}))
}

func TestRunDeploy_ExtractAndSteps(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := deployServer(t)
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	cl.pollEvery = 0

	outFile := filepath.Join(t.TempDir(), "out")
	grp := &Group{Deploy: &DeployConfig{
		Extract: map[string]string{"tag": `registry/uat/${service}:(\d{14})`},
		// ToSlash so the bash redirect target has no backslashes (a Windows
		// filepath.Join path would be mangled by bash's escape handling).
		Steps: [][]string{{"bash", "-c", "echo ${tag} > " + filepath.ToSlash(outFile)}},
	}}
	res, err := RunDeploy(context.Background(), cl, grp, "build-svc", "svc", map[string]string{"branch": "main"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Extracted["tag"] != "20260628120000" {
		t.Fatalf("tag=%q", res.Extracted["tag"])
	}
	if len(res.Steps) != 1 || res.Steps[0].Exit != 0 {
		t.Fatalf("steps=%+v", res.Steps)
	}
	b, _ := os.ReadFile(outFile)
	if strings.TrimSpace(string(b)) != "20260628120000" {
		t.Fatalf("step didn't get templated tag: %q", b)
	}
}

// stepParamEcho runs a deploy whose single step echoes ${param.limit} into a
// file, and returns what the step actually saw.
func stepParamEcho(t *testing.T, depParams, cliParams map[string]string) string {
	t.Helper()
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := deployServer(t)
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	cl.pollEvery = 0

	outFile := filepath.ToSlash(filepath.Join(t.TempDir(), "out"))
	grp := &Group{Deploy: &DeployConfig{
		Params: depParams,
		Steps:  [][]string{{"bash", "-c", "echo ${param.limit} > " + outFile}},
	}}
	if _, err := RunDeploy(context.Background(), cl, grp, "build-svc", "svc", cliParams, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// Steps template against the same params sent to Jenkins: config defaults ⊕ CLI.
func TestRunDeploy_StepsSeeConfiguredParamDefault(t *testing.T) {
	if got := stepParamEcho(t, map[string]string{"limit": "5"}, nil); got != "5" {
		t.Fatalf("step saw ${param.limit}=%q, want the configured default 5", got)
	}
}

func TestRunDeploy_StepParamCLIOverridesConfiguredDefault(t *testing.T) {
	got := stepParamEcho(t, map[string]string{"limit": "5"}, map[string]string{"limit": "9"})
	if got != "9" {
		t.Fatalf("step saw ${param.limit}=%q, want the CLI override 9", got)
	}
}

func TestRunDeploy_BinaryDenied(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := deployServer(t)
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	cl.pollEvery = 0
	grp := &Group{Deploy: &DeployConfig{Steps: [][]string{{"rm", "-rf", "/tmp/x"}}}}
	_, err := RunDeploy(context.Background(), cl, grp, "build-svc", "svc", nil, true)
	if err == nil || !strings.Contains(err.Error(), "STEP_BINARY_DENIED") {
		t.Fatalf("want STEP_BINARY_DENIED, got %v", err)
	}
}

func TestRunDeploy_ExtractNoMatch(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := deployServer(t)
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	cl.pollEvery = 0
	grp := &Group{Deploy: &DeployConfig{Extract: map[string]string{"tag": `NOPE:(\d+)`}}}
	_, err := RunDeploy(context.Background(), cl, grp, "build-svc", "svc", nil, true)
	if err == nil || !strings.Contains(err.Error(), "EXTRACT_NO_MATCH") {
		t.Fatalf("want EXTRACT_NO_MATCH, got %v", err)
	}
}
