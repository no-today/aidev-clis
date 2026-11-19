package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readJcliAuditLines returns every audit JSONL line under $AIDEV_CLIS_HOME/audit/.
func readJcliAuditLines(t *testing.T, home string) []map[string]any {
	t.Helper()
	dir := filepath.Join(home, "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read audit dir: %v", err)
	}
	var lines []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if ln == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(ln), &m); err != nil {
				t.Fatalf("bad audit line %q: %v", ln, err)
			}
			lines = append(lines, m)
		}
	}
	return lines
}

// TestAudit_BuildTwoPhase proves a build (a side-effecting op) emits a started
// line + a terminal ok line sharing one id, with request(job/params) and
// result(build/state).
func TestAudit_BuildTwoPhase(t *testing.T) {
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

	_ = runCLI(t, "build", "svc", "--wait", "--param", "BRANCH=main")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 2 {
		t.Fatalf("build must audit two lines (started+terminal), got %d: %v", len(lines), lines)
	}
	started, terminal := lines[0], lines[1]
	if started["outcome"] != "started" {
		t.Fatalf("first outcome = %v, want started", started["outcome"])
	}
	if terminal["outcome"] != "ok" {
		t.Fatalf("terminal outcome = %v, want ok", terminal["outcome"])
	}
	id1, _ := started["id"].(string)
	id2, _ := terminal["id"].(string)
	if id1 == "" || id1 != id2 {
		t.Fatalf("started/terminal must share a non-empty id: %q vs %q", id1, id2)
	}
	if cmd, _ := terminal["command"].(string); !strings.Contains(cmd, "jcli") {
		t.Fatalf("command = %q, want it to contain jcli", terminal["command"])
	}
	req, ok := terminal["request"].(map[string]any)
	if !ok {
		t.Fatalf("terminal must carry a request: %v", terminal)
	}
	if req["job"] != "build-svc" {
		t.Fatalf("request.job = %v, want build-svc", req["job"])
	}
	params, ok := req["params"].(map[string]any)
	if !ok || params["BRANCH"] != "main" {
		t.Fatalf("request.params = %v, want BRANCH=main", req["params"])
	}
	res, ok := terminal["result"].(map[string]any)
	if !ok {
		t.Fatalf("terminal must carry a result: %v", terminal)
	}
	if res["build"].(float64) != 3 {
		t.Fatalf("result.build = %v, want 3", res["build"])
	}
	if res["state"] != "SUCCESS" {
		t.Fatalf("result.state = %v, want SUCCESS", res["state"])
	}
}

// TestAudit_BuildTwoPhaseFailureKeepsPartialResult proves the failure branch of a
// two-phase op: Trigger succeeds (a started line is written) but Wait fails
// (build finished non-SUCCESS). The terminal line must be an error WITH a code,
// share the started id, and still carry result.build (partial progress) — this is
// the branch where buildResultMap on the error path matters.
func TestAudit_BuildTwoPhaseFailureKeepsPartialResult(t *testing.T) {
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
			// Wait() polls this: finished, but FAILURE → Wait returns a
			// BUILD_FAILED error with a populated result (Build=4, Result=FAILURE).
			w.Write([]byte(`{"building":false,"result":"FAILURE"}`))
		}
	}))
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	_ = runCLI(t, "build", "svc", "--wait")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 2 {
		t.Fatalf("failing build must audit two lines (started+terminal), got %d: %v", len(lines), lines)
	}
	started, terminal := lines[0], lines[1]
	if started["outcome"] != "started" {
		t.Fatalf("first outcome = %v, want started", started["outcome"])
	}
	if terminal["outcome"] != "error" {
		t.Fatalf("terminal outcome = %v, want error", terminal["outcome"])
	}
	if code, _ := terminal["code"].(string); code == "" {
		t.Fatalf("terminal error line must carry a code: %v", terminal)
	}
	id1, _ := started["id"].(string)
	id2, _ := terminal["id"].(string)
	if id1 == "" || id1 != id2 {
		t.Fatalf("started/terminal must share a non-empty id: %q vs %q", id1, id2)
	}
	res, ok := terminal["result"].(map[string]any)
	if !ok {
		t.Fatalf("failing terminal must still carry partial result: %v", terminal)
	}
	if res["build"].(float64) != 4 {
		t.Fatalf("result.build = %v, want 4 (partial progress)", res["build"])
	}
	if res["state"] != "FAILURE" {
		t.Fatalf("result.state = %v, want FAILURE", res["state"])
	}
}

// TestAudit_JobsReadSingleLine proves a read (jobs, cache miss path) emits one
// terminal line with no id.
func TestAudit_JobsReadSingleLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeJcliEnv(t, home, "http://x")

	// No jobs cache exists → LoadJobsCache errors → single error terminal line.
	_ = runCLI(t, "jobs")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("read must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if _, ok := ln["id"]; ok {
		t.Fatalf("read must have no id: %v", ln)
	}
	if cmd, _ := ln["command"].(string); !strings.Contains(cmd, "jcli") {
		t.Fatalf("command = %q, want it to contain jcli", ln["command"])
	}
}

// TestAudit_LogFetchFailureAuditsError proves `log` audits the FINAL outcome:
// ResolveBuild succeeds (so we're past the early-error paths), but the console
// fetch fails — the single terminal line must be outcome:error with a code, NOT a
// wrong "ok" written before the fetch. Locks the log audit-ordering bug.
func TestAudit_LogFetchFailureAuditsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/build-svc/api/json":
			// ResolveBuild: last build resolves fine.
			w.Write([]byte(`{"lastBuild":{"number":7}}`))
		case "/job/build-svc/7/consoleText":
			// Console fetch fails → CONSOLE_FAILED.
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	_ = runCLI(t, "log", "svc")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("log must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "error" {
		t.Fatalf("outcome = %v, want error (fetch failed after ResolveBuild)", ln["outcome"])
	}
	if code, _ := ln["code"].(string); code == "" {
		t.Fatalf("error line must carry a code: %v", ln)
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("log is a read: must have no id: %v", ln)
	}
}

// TestAudit_LogSuccessSingleOkLine proves the success path audits exactly one ok
// line (and no double-audit): the audit is written AFTER the fetch succeeds.
func TestAudit_LogSuccessSingleOkLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/build-svc/api/json":
			w.Write([]byte(`{"lastBuild":{"number":7}}`))
		case "/job/build-svc/7/consoleText":
			w.Write([]byte("hello console\n"))
		}
	}))
	defer srv.Close()
	writeJcliEnv(t, home, srv.URL)

	_ = runCLI(t, "log", "svc")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("log success must audit exactly one line, got %d: %v", len(lines), lines)
	}
	if lines[0]["outcome"] != "ok" {
		t.Fatalf("outcome = %v, want ok", lines[0]["outcome"])
	}
	if _, ok := lines[0]["id"]; ok {
		t.Fatalf("log is a read: must have no id: %v", lines[0])
	}
}

// TestAudit_BuildResolveErrorSingleLine proves an early resolve failure (unknown
// target) audits exactly one terminal error line with a code, and no started line.
func TestAudit_BuildResolveErrorSingleLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeJcliEnv(t, home, "http://x")

	_ = runCLI(t, "build", "svc", "--target", "nope")

	lines := readJcliAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("resolve error must audit exactly one line, got %d: %v", len(lines), lines)
	}
	ln := lines[0]
	if ln["outcome"] != "error" {
		t.Fatalf("outcome = %v, want error", ln["outcome"])
	}
	if code, _ := ln["code"].(string); code == "" {
		t.Fatalf("error line must carry a code: %v", ln)
	}
	if _, ok := ln["id"]; ok {
		t.Fatalf("early error must have no id (never dispatched): %v", ln)
	}
}
