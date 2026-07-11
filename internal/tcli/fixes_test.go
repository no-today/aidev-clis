package tcli

import (
	"context"
	"os"
	"testing"
	"time"
)

// ——————————————————————————————————————————
// FIX 1: log step retry
// ——————————————————————————————————————————

// TestRunLog_WithWait_Retries verifies that a log step with wait set retries on assertion failure.
// The logcli call always returns 0 records, so count >= 1 never satisfies.
// We use a tiny interval (1ms) so the test is fast, and a short timeout (50ms).
// After the run: res.Attempts > 1 (retried) and status FAILED.
func TestRunLog_WithWait_Retries(t *testing.T) {
	// non-SLS: logcli returns a log array envelope; grepRecords will filter -> [] -> count fails.
	emptyLogs := []byte(`{"data":[]}`)
	f := &fakeRunner{responses: map[string][]byte{
		"logcli targets": []byte(`{"data":[{"name":"app_logs","adapter":"docker"}]}`),
		"logcli docker":  emptyLogs, // key = name+" "+args[0]; args[0]="docker"
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "t",
		Logs: map[string]string{"app": "app_logs"},
	}
	a := Action{Name: "check_log", Log: &LogStep{
		Trace:  "my-trace-id",
		Expect: []string{"count >= 1"},
		Wait:   &Wait{Timeout: "50ms", Interval: "1ms"},
	}}
	vars := map[string]string{}
	sr, fail := r.runAction(context.Background(), c, "steps", a, vars)
	if sr.Status != "FAILED" {
		t.Fatalf("expected FAILED, got %s", sr.Status)
	}
	if sr.Attempts <= 1 {
		t.Fatalf("expected Attempts > 1 (retried), got %d", sr.Attempts)
	}
	if fail == nil || fail.Category != "assertion_failed" {
		t.Fatalf("expected assertion_failed failure, got %+v", fail)
	}
}

// TestRunLog_WithoutWait_SingleAttempt verifies that without wait, a log step runs exactly once.
func TestRunLog_WithoutWait_SingleAttempt(t *testing.T) {
	emptyLogs := []byte(`{"data":[]}`)
	f := &fakeRunner{responses: map[string][]byte{
		"logcli targets": []byte(`{"data":[{"name":"app_logs","adapter":"docker"}]}`),
		"logcli docker":  emptyLogs,
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "t",
		Logs: map[string]string{"app": "app_logs"},
	}
	a := Action{Name: "check_log", Log: &LogStep{
		Trace:  "my-trace-id",
		Expect: []string{"count >= 1"},
		// no Wait
	}}
	vars := map[string]string{}
	sr, _ := r.runAction(context.Background(), c, "steps", a, vars)
	if sr.Attempts != 1 {
		t.Fatalf("expected Attempts == 1, got %d", sr.Attempts)
	}
}

// ——————————————————————————————————————————
// FIX 2: inline actor temp dir cleanup
// ——————————————————————————————————————————

// TestRunCases_InlineActorTmpDirCleaned verifies that after RunCases completes,
// the inline-actor temp dir is removed.
func TestRunCases_InlineActorTmpDirCleaned(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":200,"body":{}}}`),
	}}
	r := newTestRunner(f)

	// Manually create tmp dir to simulate inline actor usage.
	dir, err := os.MkdirTemp("", "tcli-actors-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	r.tmpDir = dir

	// Write a v2 case file.
	caseDir := t.TempDir()
	caseYAML := `name: inline-actor-test
apps:
  orders:
    actor:
      vars:
        user: testuser
steps:
  - name: call
    api:
      request: "GET /health"
      expect:
        - "status_code == 200"
`
	caseFile := caseDir + "/case.yaml"
	if err := os.WriteFile(caseFile, []byte(caseYAML), 0o600); err != nil {
		t.Fatalf("write case: %v", err)
	}

	ctx := context.Background()
	_, _, _ = RunCases(ctx, r, caseFile, nil)

	// After RunCases, the temp dir must be gone.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected tmpDir %s to be removed, stat err: %v", dir, err)
	}
}

// TestRunCases_NoInlineActorNoTmpDir verifies that when no inline actor is used,
// r.tmpDir stays empty and no cleanup panic occurs.
func TestRunCases_NoInlineActorNoTmpDir(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":200,"body":{}}}`),
	}}
	r := newTestRunner(f)

	caseDir := t.TempDir()
	caseYAML := `name: no-actor-test
apps:
  orders: {}
steps:
  - name: call
    api:
      request: "GET /health"
      expect:
        - "status_code == 200"
`
	caseFile := caseDir + "/case.yaml"
	if err := os.WriteFile(caseFile, []byte(caseYAML), 0o600); err != nil {
		t.Fatalf("write case: %v", err)
	}

	_, _, _ = RunCases(context.Background(), r, caseFile, nil)
	// No panic; r.tmpDir was never set — nothing to verify except no crash.
}

// ——————————————————————————————————————————
// FIX 3: failure Type always set
// ——————————————————————————————————————————

// TestRunPhase_FailureTypeAlwaysSet verifies that failures produced by runPhase
// always have Type set to the action's kind.
func TestRunPhase_FailureTypeAlwaysSet(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":500,"body":{}}}`),
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
	}
	a := Action{Name: "x", API: &APIStep{
		Request: "GET /x",
		Expect:  []string{"status_code == 200"},
	}}
	_, f2 := r.runPhase(context.Background(), c, "steps", []Action{a}, map[string]string{}, true)
	if f2 == nil {
		t.Fatal("expected failure")
	}
	if f2.Type == "" {
		t.Fatalf("Failure.Type must be set, got empty; failure=%+v", f2)
	}
	if f2.Type != "api" {
		t.Fatalf("Failure.Type: want 'api', got %q", f2.Type)
	}
}

// TestRunPhase_AssertScriptFailureTypeSet verifies that assert_script failures
// produced via runWithRetry also get Type stamped in runPhase.
func TestRunPhase_AssertScriptFailureTypeSet(t *testing.T) {
	if testing.Short() {
		t.Skip("requires /usr/bin/false or similar")
	}
	f := &fakeRunner{responses: map[string][]byte{
		// API returns OK so we reach assert_script
		"apicli call": []byte(`{"data":{"status_code":200,"body":{}}}`),
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
	}
	a := Action{
		Name: "x",
		API: &APIStep{
			Request: "GET /x",
			Expect:  []string{"status_code == 200"},
			Assert:  &ScriptAssert{Command: "false"},
		},
	}
	_, fail := r.runPhase(context.Background(), c, "steps", []Action{a}, map[string]string{}, true)
	if fail == nil {
		t.Fatal("expected failure from assert_script")
	}
	if fail.Type == "" {
		t.Fatalf("Failure.Type must be set for assert_script, got empty; failure=%+v", fail)
	}
	if fail.Type != "api" {
		t.Fatalf("Failure.Type: want 'api', got %q", fail.Type)
	}
}

// ——————————————————————————————————————————
// FIX 4: grepRecords returns [] on zero matches
// ——————————————————————————————————————————

// TestGrepRecords_EmptyReturnsArray verifies that grepRecords returns "[]" (not "null")
// when no records match the needle.
func TestGrepRecords_EmptyReturnsArray(t *testing.T) {
	data := []byte(`[{"message":"hello world"},{"message":"foobar"}]`)
	result := grepRecords(data, "NOPE_NOT_FOUND")
	if string(result) != "[]" {
		t.Fatalf("want '[]', got %q", string(result))
	}
}

// TestGrepRecords_ZeroCountEvaluatesCorrectly verifies that count >= 1
// correctly evaluates to failure when grepRecords returns [].
func TestGrepRecords_ZeroCountEvaluatesCorrectly(t *testing.T) {
	result := grepRecords([]byte(`[{"message":"hello"}]`), "NOPE")
	if string(result) != "[]" {
		t.Fatalf("want '[]', got %q", string(result))
	}
	// count >= 1 on empty array should fail
	err := EvalExprs(result, []string{"count >= 1"}, "", nil)
	if err == nil {
		t.Fatal("count >= 1 on empty array must fail")
	}
}

// TestGrepRecords_NonEmptyMatch verifies normal matching still works.
func TestGrepRecords_NonEmptyMatch(t *testing.T) {
	data := []byte(`[{"message":"trace-abc found"},{"message":"other"}]`)
	result := grepRecords(data, "trace-abc")
	if string(result) == "null" || string(result) == "[]" {
		t.Fatalf("expected 1 match, got %q", string(result))
	}
	// count == 1 should pass
	if err := EvalExprs(result, []string{"count == 1"}, "", nil); err != nil {
		t.Fatalf("count == 1 should pass: %v", err)
	}
}

// ——————————————————————————————————————————
// FIX (review): ctx-aware wait interval
// ——————————————————————————————————————————

// TestSleepCtx_CancelReturnsPromptly verifies sleepCtx honors ctx cancellation
// well before the full duration elapses.
func TestSleepCtx_CancelReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	start := time.Now()
	if ok := sleepCtx(ctx, 10*time.Second); ok {
		t.Fatal("sleepCtx should report false when ctx cancelled")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("sleepCtx did not return promptly: %v", el)
	}
}

func TestSleepCtx_FullElapse(t *testing.T) {
	if !sleepCtx(context.Background(), 2*time.Millisecond) {
		t.Fatal("sleepCtx should report true when the interval elapses")
	}
}

// TestRunLog_CtxCancelledDuringWait verifies a log step whose ctx is cancelled
// mid wait-interval returns promptly instead of blocking the full interval.
func TestRunLog_CtxCancelledDuringWait(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"logcli targets": []byte(`{"data":[{"name":"app_logs","adapter":"docker"}]}`),
		"logcli docker":  []byte(`{"data":[]}`), // count >= 1 never satisfied -> retry
	}}
	r := newTestRunner(f)
	c := &Case{Name: "t", Logs: map[string]string{"app": "app_logs"}}
	a := Action{Name: "check_log", Log: &LogStep{
		Trace:  "my-trace-id",
		Expect: []string{"count >= 1"},
		Wait:   &Wait{Timeout: "60s", Interval: "10s"}, // huge interval; ctx must cut it short
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	res, fail := r.runAction(ctx, c, "steps", a, map[string]string{})
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("runLog ignored ctx cancellation during wait: took %v", el)
	}
	if res.Status != "FAILED" || fail == nil {
		t.Fatalf("want FAILED failure on cancel, got status=%s fail=%+v", res.Status, fail)
	}
}

// ——————————————————————————————————————————
// FIX (review): grep falls back when "message" is absent
// ——————————————————————————————————————————

// TestGrepRecords_FallbackWhenNoMessageField verifies that a record without a
// "message" key still matches against its raw text, avoiding a silent zero-match.
func TestGrepRecords_FallbackWhenNoMessageField(t *testing.T) {
	data := []byte(`[{"content":"trace-abc happened"},{"content":"unrelated"}]`)
	out := grepRecords(data, "trace-abc")
	if err := EvalExprs(out, []string{"count == 1"}, "", nil); err != nil {
		t.Fatalf("fallback match failed, out=%q err=%v", out, err)
	}
}

// TestGrepRecords_MessagePresentStaysScoped verifies that when "message" exists,
// matching stays scoped to it (a needle only in another field does not match).
func TestGrepRecords_MessagePresentStaysScoped(t *testing.T) {
	data := []byte(`[{"message":"hello","other":"trace-abc"}]`)
	out := grepRecords(data, "trace-abc")
	if string(out) != "[]" {
		t.Fatalf("message-scoped match should not match other fields, got %q", out)
	}
}

func TestRunAction_ElapsedMSStampedOnReturnedResult(t *testing.T) {
	// inject deterministic clock: each call +5ms
	orig := nowMS
	defer func() { nowMS = orig }()
	var clock int64
	nowMS = func() int64 { clock += 5; return clock }
	r := newTestRunner(&fakeRunner{})
	// standalone assertion: no CLI call needed
	a := Action{Name: "eq", Expect: []string{"a == a"}}
	res, _ := r.runAction(context.Background(), &Case{Name: "t"}, "assertions", a, map[string]string{})
	if res.ElapsedMS != 5 {
		t.Fatalf("ElapsedMS = %d, want 5 (start=5, end=10)", res.ElapsedMS)
	}
}
