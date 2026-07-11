package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/apicli"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// buildApicli compiles the apicli binary into a temp dir and returns its path.
// Used by subprocess tests where the raw-error path calls os.Exit (which would
// kill the in-process test harness).
func buildApicli(t *testing.T) string {
	t.Helper()
	name := "apicli"
	if runtime.GOOS == "windows" {
		name += ".exe" // Windows won't exec an extensionless binary path
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build apicli: %v\n%s", err, out)
	}
	return bin
}

// TestCall_RawErrorIsPlainLine proves a raw-mode transport error is a plain
// "ERROR ..." line, not a JSON envelope. It runs the built binary as a
// subprocess because the raw-error handler calls os.Exit, which would
// terminate an in-process test.
func TestCall_RawErrorIsPlainLine(t *testing.T) {
	bin := buildApicli(t)
	home := t.TempDir()
	// base_url points at a host that can't be reached → apicli.Call returns a
	// transport error (an err != nil return, not a business failure).
	writeApicliYAML(t, home, "http://127.0.0.1:1")
	cmd := exec.Command(bin, "call", "shop", "/api/x", "--output", "raw")
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

func runCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	root := newRoot()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		// Mirror main(): on error, the error envelope is written to stdout.
		e := errs.From(err)
		_ = envelope.WriteError(os.Stdout, e.Code, e.Message, pretty)
		t.Logf("execute error: %v", err)
	}

	_ = w.Close()
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return buf[:n]
}

// writeApicliYAML writes a minimal no-auth apicli.yaml for app "shop" pointing at
// baseURL, with an ok_when that matches body.code == 0.
func writeApicliYAML(t *testing.T, home, baseURL string) {
	t.Helper()
	cfg := `apps:
  shop:
    base_url: ` + baseURL + `
    auth: { kind: none }
    response: { ok_when: "body.code == 0" }
`
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

// captureStderr swaps os.Stderr for an os.Pipe() around fn and returns the
// captured text. Mirrors how runCLI captures os.Stdout (single short read).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	_ = w.Close()
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// writeTraceApp writes app "shop" with a trace_field + log_env so that a
// {"code":1} response is a business failure carrying a trace id.
func writeTraceApp(t *testing.T, home, baseURL string) {
	t.Helper()
	cfg := `apps:
  shop:
    base_url: ` + baseURL + `
    auth: { kind: none }
    response: { ok_when: "body.code == 0" }
    trace_field: header.X-Trace-Id
    log_env: log_uat
`
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCallEmitsTraceHintOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Trace-Id", "tr-7")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":1}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeTraceApp(t, home, srv.URL)
	stderr := captureStderr(t, func() {
		_ = runCLI(t, "call", "shop", "/x", "--base-url", srv.URL)
	})
	if !strings.Contains(stderr, "tr-7") || !strings.Contains(stderr, "logcli sls trace tr-7") {
		t.Fatalf("expected trace pivot hint on stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "--target log_uat") {
		t.Fatalf("hint should include log_env as a logcli --target: %q", stderr)
	}
}

func TestCallEndToEndNoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":7}}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	out := runCLI(t, "call", "shop", "/api/x", "--output", "json")
	var env struct {
		Data struct {
			OK   bool `json:"ok"`
			Body struct {
				Data struct{ ID int } `json:"data"`
			} `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if !env.Data.OK || env.Data.Body.Data.ID != 7 {
		t.Errorf("got %+v", env.Data)
	}
}

func TestCall_RejectsBadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, "http://unused.example")
	// Validation runs before any network call, so no test server is needed.
	out := runCLI(t, "call", "shop", "/api/x", "--output", "bogus")
	if !bytes.Contains(out, []byte("UNSUPPORTED_OUTPUT")) {
		t.Fatalf("expected UNSUPPORTED_OUTPUT for --output bogus, got: %s", out)
	}
}

func TestCallOutputFileEnvelopeIsMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("BINARYDATA"))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)
	out := filepath.Join(home, "dl.bin")
	raw := runCLI(t, "call", "shop", "/export", "--base-url", srv.URL, "--output-file", out)
	var env struct {
		Data struct {
			BodyFile  string `json:"body_file"`
			BodyBytes int64  `json:"body_bytes"`
			SHA256    string `json:"sha256"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad envelope: %s", raw)
	}
	if env.Data.BodyFile != out || env.Data.BodyBytes != 10 || env.Data.SHA256 == "" {
		t.Fatalf("metadata wrong: %+v", env.Data)
	}
}

func TestCallCrossOriginGuardAndOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":9}}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	// App base points at a DIFFERENT host than the request URL below.
	cfg := `apps:
  shop:
    base_url: http://ignored.example
    auth: { kind: none }
    response: { ok_when: "body.code == 0" }
`
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without the flag: an absolute URL to another host is blocked.
	out := runCLI(t, "call", "shop", srv.URL+"/api/x", "--output", "json")
	if !bytes.Contains(out, []byte("API_CROSS_ORIGIN_URL")) {
		t.Fatalf("expected cross-origin guard to fire, got: %s", out)
	}

	// With --allow-cross-origin: the request goes through and succeeds.
	out = runCLI(t, "call", "shop", srv.URL+"/api/x", "--output", "json", "--allow-cross-origin")
	if bytes.Contains(out, []byte("API_CROSS_ORIGIN_URL")) {
		t.Fatalf("--allow-cross-origin should override the guard, got: %s", out)
	}
	var env struct {
		Data struct {
			OK   bool `json:"ok"`
			Body struct {
				Data struct{ ID int } `json:"data"`
			} `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if !env.Data.OK || env.Data.Body.Data.ID != 9 {
		t.Errorf("got %+v", env.Data)
	}
}

func TestCallBusinessFailureSingleEnvelope(t *testing.T) {
	businessExit = 0 // reset package global for test isolation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":4001,"msg":"nope"}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := `apps:
  svc-open:
    base_url: ` + srv.URL + `
    auth: { kind: none }
    response: { ok_when: "body.code == 0" }
`
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "call", "svc-open", "/api/x")
	if bytes.Contains(out, []byte(`"error"`)) {
		t.Errorf("business failure must NOT emit an {error} envelope: %s", out)
	}
	var env struct {
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("expected a single {data} envelope, got: %s (%v)", out, err)
	}
	if env.Data.OK {
		t.Errorf("expected ok=false on business failure")
	}
	if businessExit != 1 {
		t.Errorf("expected businessExit=1, got %d", businessExit)
	}
}

// TestLoginActorFileVarsUsed proves --actor-file vars reach the request: the
// login flow substitutes {{phoneNo}} from tg.Vars into the request body. The
// actors.yaml account says "144"; the inline actor file says "139" and must
// REPLACE (not merge with) it.
func TestLoginActorFileVarsUsed(t *testing.T) {
	var sawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" {
			b, _ := io.ReadAll(r.Body)
			sawBody = string(b)
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"tok123"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := `apps:
  svc-login:
    base_url: ` + srv.URL + `
    default_actor: alice
    auth:
      kind: flow
      vars_required: [phoneNo]
      flow:
        - request: |
            POST /auth/login
            Content-Type: application/json

            {"phoneNo":"{{phoneNo}}"}
          capture:
            token: body.data.token
      inject:
        header: "Authorization: Bearer {{token}}"
    response:
      ok_when: "body.code == 0"
`
	actors := "actors:\n  svc-login:\n    alice: { phoneNo: \"144\" }\n"
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "actors.yaml"), []byte(actors), 0o600); err != nil {
		t.Fatal(err)
	}
	actorFile := filepath.Join(home, "oneoff.yaml")
	if err := os.WriteFile(actorFile, []byte("vars:\n  phoneNo: \"139\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "login", "svc-login", "--actor-file", actorFile)
	if !bytes.Contains(out, []byte(`"status":"logged-in"`)) {
		t.Fatalf("login = %s", out)
	}
	if !strings.Contains(sawBody, `"phoneNo":"139"`) {
		t.Fatalf("inline actor phoneNo should reach request; body = %q", sawBody)
	}
	if strings.Contains(sawBody, "144") {
		t.Fatalf("actors.yaml value must NOT merge in; body = %q", sawBody)
	}
}

func TestLoginVarEmitsArgvWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeOTPLoginYAML(t, home, srv.URL)
	raw := runCLI(t, "login", "shop", "--base-url", srv.URL, "--var", "verifyCode=9999")
	var env struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad envelope: %s", raw)
	}
	found := false
	for _, w := range env.Warnings {
		if strings.Contains(w, "argv") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an argv visibility warning, got: %s", raw)
	}
}

// writeOTPLoginYAML writes a flow app "shop" whose flow posts
// {{verifyCode}} (a per-invocation one-time value) and captures a token.
// verifyCode is listed in vars_required but is NOT in any actors.yaml account,
// so it can only be supplied at login time via --var.
func writeOTPLoginYAML(t *testing.T, home, baseURL string) {
	t.Helper()
	cfg := `apps:
  shop:
    base_url: ` + baseURL + `
    default_actor: alice
    auth:
      kind: flow
      vars_required: [verifyCode]
      flow:
        - request: |
            POST /login
            Content-Type: application/json

            {"verifyCode":"{{verifyCode}}"}
          capture:
            token: body.token
      inject:
        header: "Authorization: Bearer {{token}}"
    response:
      ok_when: "body.token != null"
`
	actors := "actors:\n  shop:\n    alice: {}\n"
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "actors.yaml"), []byte(actors), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoginVarSuppliesOneTimeValue(t *testing.T) {
	var sawCode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("9999")) {
			sawCode = "9999"
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeOTPLoginYAML(t, home, srv.URL)
	_ = runCLI(t, "login", "shop", "--base-url", srv.URL, "--var", "verifyCode=9999")
	if sawCode != "9999" {
		t.Fatalf("--var verifyCode was not sent in the login flow")
	}
}

func TestLoginWhoamiLogoutRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"tok123"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := `apps:
  svc-login:
    base_url: ` + srv.URL + `
    default_actor: alice
    auth:
      kind: flow
      vars_required: [phoneNo]
      flow:
        - request: |
            POST /auth/login
            Content-Type: application/json

            {"phoneNo":"{{phoneNo}}"}
          capture:
            token: body.data.token
      inject:
        header: "Authorization: Bearer {{token}}"
    response:
      ok_when: "body.code == 0"
      expired_when: "body.code == 401"
`
	actors := "actors:\n  svc-login:\n    alice: { phoneNo: \"144\" }\n"
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "actors.yaml"), []byte(actors), 0o600); err != nil {
		t.Fatal(err)
	}

	// before login: whoami reports not logged in
	out := runCLI(t, "whoami", "svc-login")
	if !bytes.Contains(out, []byte(`"logged_in":false`)) {
		t.Errorf("whoami pre-login = %s", out)
	}
	// login establishes a session
	out = runCLI(t, "login", "svc-login")
	if !bytes.Contains(out, []byte(`"status":"logged-in"`)) {
		t.Errorf("login = %s", out)
	}
	// whoami now reports logged in
	out = runCLI(t, "whoami", "svc-login")
	if !bytes.Contains(out, []byte(`"logged_in":true`)) {
		t.Errorf("whoami post-login = %s", out)
	}
	// logout removes the session
	out = runCLI(t, "logout", "svc-login")
	if !bytes.Contains(out, []byte(`"status":"logged-out"`)) {
		t.Errorf("logout = %s", out)
	}
	out = runCLI(t, "whoami", "svc-login")
	if !bytes.Contains(out, []byte(`"logged_in":false`)) {
		t.Errorf("whoami post-logout = %s", out)
	}
}

// readAuditLines returns the parsed audit JSONL lines written under home during
// a test (a single UTC day-file; tests are short-lived so there's exactly one).
func readAuditLines(t *testing.T, home string) []map[string]any {
	t.Helper()
	dir := filepath.Join(home, "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read audit dir %s: %v", dir, err)
	}
	var lines []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
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

// TestCallAuditGETSingleLine proves a GET call (a read) produces exactly one
// terminal audit line: no id, outcome ok, result.status set, request carries
// method/url and the -H header (minus Cookie), command mentions apicli.
func TestCallAuditGETSingleLine(t *testing.T) {
	businessExit = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	_ = runCLI(t, "call", "shop", "/api/x", "-H", "X-Req: 1", "-H", "Cookie: sess=abc")

	lines := readAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("GET call should be a single audit line, got %d: %+v", len(lines), lines)
	}
	ln := lines[0]
	if _, has := ln["id"]; has {
		t.Errorf("read (GET) must not carry an id: %+v", ln)
	}
	if ln["outcome"] != "ok" {
		t.Errorf("outcome = %v, want ok", ln["outcome"])
	}
	cmd, _ := ln["command"].(string)
	if !strings.Contains(cmd, "apicli") {
		t.Errorf("command should mention apicli: %q", cmd)
	}
	res, ok := ln["result"].(map[string]any)
	if !ok || res["status"] == nil {
		t.Fatalf("result.status missing: %+v", ln["result"])
	}
	if int(res["status"].(float64)) != 200 {
		t.Errorf("result.status = %v, want 200", res["status"])
	}
	req, ok := ln["request"].(map[string]any)
	if !ok {
		t.Fatalf("request missing: %+v", ln)
	}
	if req["method"] != "GET" {
		t.Errorf("request.method = %v, want GET", req["method"])
	}
	if u, _ := req["url"].(string); u == "" {
		t.Errorf("request.url should be non-empty: %+v", req)
	}
	// actor must survive into the audit line so a future refactor can't silently
	// drop it (app "shop" has no default_actor, so the value is "" — the KEY must
	// still be present).
	if _, has := req["actor"]; !has {
		t.Errorf("request.actor key should be present: %+v", req)
	}
	hdrs, ok := req["headers"].(map[string]any)
	if !ok {
		t.Fatalf("request.headers should be present: %+v", req)
	}
	if hdrs["X-Req"] != "1" {
		t.Errorf("request.headers should carry X-Req: %+v", hdrs)
	}
	for k := range hdrs {
		if strings.EqualFold(k, "Cookie") {
			t.Errorf("request.headers must NOT include Cookie: %+v", hdrs)
		}
	}
}

// TestCallAuditPOSTTwoPhase proves a non-GET call (a write) produces two audit
// lines sharing an id: a "started" line then a terminal "ok" line with result.
func TestCallAuditPOSTTwoPhase(t *testing.T) {
	businessExit = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL)

	_ = runCLI(t, "call", "shop", "/api/x", "-X", "POST", "-d", `{"a":1}`)

	lines := readAuditLines(t, home)
	if len(lines) != 2 {
		t.Fatalf("POST call should be two audit lines (started+terminal), got %d: %+v", len(lines), lines)
	}
	started, terminal := lines[0], lines[1]
	if started["outcome"] != "started" {
		t.Errorf("first line should be started, got %v", started["outcome"])
	}
	if terminal["outcome"] != "ok" {
		t.Errorf("terminal line should be ok, got %v", terminal["outcome"])
	}
	id1, _ := started["id"].(string)
	id2, _ := terminal["id"].(string)
	if id1 == "" || id1 != id2 {
		t.Errorf("started/terminal should share a non-empty id: %q vs %q", id1, id2)
	}
	res, ok := terminal["result"].(map[string]any)
	if !ok || int(res["status"].(float64)) != 201 {
		t.Fatalf("terminal result.status = %v, want 201", terminal["result"])
	}
	if terminal["request"].(map[string]any)["method"] != "POST" {
		t.Errorf("request.method = %v, want POST", terminal["request"])
	}
}

// TestCallAuditAbsoluteURLNotMangled proves the audited request.url for an
// absolute (cross-origin) path is the URL actually sent — NOT the app base with
// the absolute URL wrongly appended. Also asserts request.actor carries the
// configured default_actor value into the audit line.
func TestCallAuditAbsoluteURLNotMangled(t *testing.T) {
	businessExit = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	// App base points at a DIFFERENT host than the request URL below; a default
	// actor is set so we can assert request.actor.
	cfg := `apps:
  shop:
    base_url: http://ignored.example
    default_actor: alice
    auth: { kind: none }
    response: { ok_when: "body.code == 0" }
`
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	absURL := srv.URL + "/api/x"
	_ = runCLI(t, "call", "shop", absURL, "--allow-cross-origin")

	lines := readAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("GET cross-origin call should be a single audit line, got %d: %+v", len(lines), lines)
	}
	req, ok := lines[0]["request"].(map[string]any)
	if !ok {
		t.Fatalf("request missing: %+v", lines[0])
	}
	if req["url"] != absURL {
		t.Errorf("audit request.url = %v, want the un-mangled absolute URL %q", req["url"], absURL)
	}
	if req["actor"] != "alice" {
		t.Errorf("request.actor = %v, want alice", req["actor"])
	}
}

// TestCallAuditCurlPreviewSingleLine proves --curl (a preview that never hits the
// backend) is a single, non-side-effecting terminal line with no result.
func TestCallAuditCurlPreviewSingleLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, "http://unused.example")

	_ = runCLI(t, "call", "shop", "/api/x", "-X", "POST", "--curl")

	lines := readAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("--curl preview should be a single audit line, got %d: %+v", len(lines), lines)
	}
	if lines[0]["outcome"] != "ok" {
		t.Errorf("curl preview outcome = %v, want ok", lines[0]["outcome"])
	}
	if _, has := lines[0]["id"]; has {
		t.Errorf("curl preview is not side-effecting → no id: %+v", lines[0])
	}
	if _, has := lines[0]["result"]; has {
		t.Errorf("curl preview has no result: %+v", lines[0])
	}
}

func TestLoginWhoamiLogoutAuditSingleLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, srv.URL) // no-auth app: whoami/logout still audit

	_ = runCLI(t, "whoami", "shop")
	lines := readAuditLines(t, home)
	if len(lines) != 1 {
		t.Fatalf("whoami should audit one line, got %d: %+v", len(lines), lines)
	}
	if lines[0]["outcome"] != "ok" {
		t.Errorf("whoami outcome = %v, want ok", lines[0]["outcome"])
	}
	if _, has := lines[0]["request"]; has {
		t.Errorf("whoami should have no request layer: %+v", lines[0])
	}
	if _, has := lines[0]["id"]; has {
		t.Errorf("whoami is not side-effecting → no id: %+v", lines[0])
	}
}

func TestWhoamiReportsExpiryRiskAndCapturedVars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeApicliYAML(t, home, "http://unused")
	// writeApicliYAML defines app "shop" with no default_actor, so `whoami shop`
	// resolves actor "" unless --actor is given. We pass --actor demo and seed the
	// session under the same (app=shop, actor=demo, env="" -> "_default") key.
	tg := &apicli.Target{App: "shop", Actor: "demo"}
	if err := apicli.SaveSession(tg, apicli.Session{Vars: map[string]string{"token": "x", "accessToken": "y"}}); err != nil {
		t.Fatal(err)
	}
	raw := runCLI(t, "whoami", "shop", "--actor", "demo")
	var env struct {
		Data struct {
			ExpiryRisk   string   `json:"expiry_risk"`
			CapturedVars []string `json:"captured_vars"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad envelope: %s", raw)
	}
	if env.Data.ExpiryRisk != "low" {
		t.Fatalf("fresh session should be low risk, got %q", env.Data.ExpiryRisk)
	}
	got := strings.Join(env.Data.CapturedVars, ",")
	if !strings.Contains(got, "token") || !strings.Contains(got, "accessToken") {
		t.Fatalf("captured_vars should list keys: %v", env.Data.CapturedVars)
	}
}

// A method verb pasted into the path used to be requested literally as the URL
// ("POST /x" → 200 HTML from the SPA fallback — silent wrong result). Reject it.
func TestValidatePath_VerbAndWhitespace(t *testing.T) {
	err := validatePath("POST /hsytpay/joblog/pageList")
	if err == nil || !strings.Contains(err.Error(), "-X POST") {
		t.Fatalf("verb-in-path must error with the -X hint, got %v", err)
	}
	if err := validatePath("/a b/c"); err == nil {
		t.Fatal("whitespace in path must error")
	}
	if err := validatePath("/ok/path?x=1&y=2"); err != nil {
		t.Fatalf("normal path must pass: %v", err)
	}
	// a path segment that merely starts with a verb-like word is fine
	if err := validatePath("/get/things"); err != nil {
		t.Fatalf("verb-like segment must pass: %v", err)
	}
}

func TestAppArgs_MissingPointsAtApps(t *testing.T) {
	v := appArgs(1, "")
	err := v(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "apicli apps") {
		t.Fatalf("missing app must point at `apicli apps`, got %v", err)
	}
	if err := v(nil, []string{"app1"}); err != nil {
		t.Fatalf("exact args must pass: %v", err)
	}
	if err := v(nil, []string{"app1", "extra"}); err == nil {
		t.Fatal("too many args must error")
	}
}

// `apicli apps` lists configured apps + actors without touching any backend.
func TestAppsCmd_ListsAppsAndActors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "apicli.yaml"), []byte(`apps:
  app1:
    base_url: https://app1.example.com
    default_actor: default
    auth: {kind: flow}
  app2:
    base_url: https://app2.example.com
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "actors.yaml"), []byte(`actors:
  app1:
    default: {u: a}
    admin: {u: b}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "apps")
	var env struct {
		Data []struct {
			App     string   `json:"app"`
			BaseURL string   `json:"base_url"`
			Actors  []string `json:"actors"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not a {data} envelope: %s", out)
	}
	if len(env.Data) != 2 || env.Data[0].App != "app1" || env.Data[1].App != "app2" {
		t.Fatalf("want sorted [app1 app2], got %+v", env.Data)
	}
	if strings.Join(env.Data[0].Actors, ",") != "admin,default" {
		t.Fatalf("app1 actors: %v", env.Data[0].Actors)
	}
}
