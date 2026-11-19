package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildJcli compiles the jcli binary into a temp dir and returns its path. Used
// by subprocess tests where the raw-error path calls os.Exit (which would kill
// the in-process test harness).
func buildJcli(t *testing.T) string {
	t.Helper()
	name := "jcli"
	if runtime.GOOS == "windows" {
		name += ".exe" // Windows won't exec an extensionless binary path
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build jcli: %v\n%s", err, out)
	}
	return bin
}

// TestLogCmd_RawErrorIsPlainLine proves a raw-mode error is a plain "ERROR ..."
// line, not a JSON envelope. It runs the built binary as a subprocess because
// the raw-error handler calls os.Exit, which would terminate an in-process test.
func TestLogCmd_RawErrorIsPlainLine(t *testing.T) {
	bin := buildJcli(t)
	home := t.TempDir() // no jcli.yaml here → resolveTarget fails
	cmd := exec.Command(bin, "log", "svc", "--output", "raw")
	cmd.Env = append(os.Environ(), "AIDEV_CLIS_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got success; out=%q", out)
	}
	got := string(out)
	if !strings.HasPrefix(got, "ERROR ") {
		t.Fatalf("raw error should be a plain ERROR line, got %q", got)
	}
	if strings.Contains(got, "{") {
		t.Fatalf("raw error must not be an envelope: %q", got)
	}
}

// logTestServer serves the two endpoints `jcli log` hits: lastBuild resolution
// and the console text for that build.
func logTestServer(t *testing.T, console string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/job/build-svc/api/json":
			w.Write([]byte(`{"lastBuild":{"number":12}}`))
		case r.URL.Path == "/job/build-svc/12/consoleText":
			w.Write([]byte(console))
		}
	}))
}

func TestLogCmd_RawPrintsRawConsole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	const console = "Started\nBuilding...\nFinished: SUCCESS\n"
	srv := logTestServer(t, console)
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	out := runCLIRaw(t, "log", "svc", "--output", "raw")
	if out != console {
		t.Fatalf("plain output should be the raw console verbatim\nwant: %q\ngot:  %q", console, out)
	}
}

func TestLogCmd_RejectsBadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeJcliEnv(t, home, "http://example.invalid")
	// Validation runs before any network call, so no test server is needed.
	if e, _ := runCLI(t, "log", "svc", "--output", "bogus")["error"].(map[string]any); e == nil || e["code"] != "UNSUPPORTED_OUTPUT" {
		t.Fatalf("want error code UNSUPPORTED_OUTPUT, got: %v", e)
	}
}

func TestLogCmd_DefaultWrapsInEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	const console = "Started\nFinished: SUCCESS\n"
	srv := logTestServer(t, console)
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	env := runCLI(t, "log", "svc")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data envelope: %v", env)
	}
	if data["console"] != console {
		t.Fatalf("console field: %q", data["console"])
	}
	if data["build"].(float64) != 12 {
		t.Fatalf("build: %v", data["build"])
	}
	// The whole console must be a single JSON string field, not raw lines.
	if strings.HasPrefix(strings.TrimSpace(runCLIRaw(t, "log", "svc")), "Started") {
		t.Fatalf("default mode should not print raw console text")
	}
}
