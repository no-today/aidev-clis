package tcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// writeCase is a tiny helper to drop a v2 case file into dir.
func writeCase(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write case %s: %v", name, err)
	}
}

const passingCaseYAML = `name: tagged
tags: [smoke]
apps:
  orders: {}
steps:
  - name: call
    api:
      request: "GET /health"
      expect:
        - "status_code == 200"
`

// TestRunCases_DirTagFilterZero_NoCase verifies the CI gate hazard fix: a
// directory whose cases are all excluded by the --tag filter must return
// NO_CASE / exit 2, NOT a green "0/0" PASS suite.
func TestRunCases_DirTagFilterZero_NoCase(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":200,"body":{}}}`),
	}}
	r := newTestRunner(f)
	dir := t.TempDir()
	writeCase(t, dir, "a.yaml", passingCaseYAML)

	payload, exit, err := RunCases(context.Background(), r, dir, []string{"nonexistent-tag"})
	if err == nil {
		t.Fatalf("expected NO_CASE error, got payload=%+v exit=%d", payload, exit)
	}
	if e := errs.From(err); e.Code != "NO_CASE" {
		t.Fatalf("want code NO_CASE, got %q", e.Code)
	}
	if exit != errs.ExitConfig {
		t.Fatalf("want exit %d (config), got %d", errs.ExitConfig, exit)
	}
	if len(f.calls) != 0 {
		t.Fatalf("no case should have run, but got calls: %v", f.calls)
	}
}

// TestRunCases_DirEmpty_NoCase verifies an empty directory also returns NO_CASE
// rather than a PASS suite.
func TestRunCases_DirEmpty_NoCase(t *testing.T) {
	r := newTestRunner(&fakeRunner{})
	dir := t.TempDir()
	_, exit, err := RunCases(context.Background(), r, dir, nil)
	if err == nil || errs.From(err).Code != "NO_CASE" || exit != errs.ExitConfig {
		t.Fatalf("empty dir must yield NO_CASE/exit2, got exit=%d err=%v", exit, err)
	}
}

// TestRunCases_DirTagMatch_Runs is the positive control: a matching tag yields a
// suite result and exit reflects the verdict.
func TestRunCases_DirTagMatch_Runs(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":200,"body":{}}}`),
	}}
	r := newTestRunner(f)
	dir := t.TempDir()
	writeCase(t, dir, "a.yaml", passingCaseYAML)
	payload, exit, err := RunCases(context.Background(), r, dir, []string{"smoke"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := payload.(SuiteResult); !ok {
		t.Fatalf("want SuiteResult, got %T", payload)
	}
	if exit != errs.ExitOK {
		t.Fatalf("want exit 0, got %d", exit)
	}
}

func TestRunCase_PassAPIThenDB(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"dbcli targets": []byte(`{"data":[{"name":"orders_uat","adapter":"mysql"}]}`),
		"apicli call":   []byte(`{"data":{"status_code":200,"body":{"id":"ORD-1"}}}`),
		"dbcli mysql":   []byte(`{"data":{"columns":["status"],"rows":[["confirmed"]]}}`),
	}}
	r := newTestRunner(f)
	r.runID = "TEST-RID"
	c := &Case{
		Name: "smoke",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
		DBs:  map[string]string{"main": "orders_uat"},
		Steps: []Action{
			{Name: "create", API: &APIStep{
				Request: "POST /orders",
				Expect:  []string{"status_code == 200"},
				Capture: map[string]string{"order_id": "body.id"},
			}},
			{Name: "verify", DB: &DBStep{
				SQL:    "SELECT status FROM orders WHERE id='{{order_id}}'",
				Expect: []string{"count == 1", "rows.0.0 == confirmed"},
			}},
		},
	}
	cr := r.RunCase(context.Background(), c)
	if cr.Verdict != "PASS" || cr.Counts.Passed != 2 {
		t.Fatalf("verdict=%s counts=%+v fail=%+v", cr.Verdict, cr.Counts, cr.Failure)
	}
}

func TestRunCase_StepsFailSkipsAssertionsRunsCleanup(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":500,"body":{}}}`),
	}}
	r := newTestRunner(f)
	r.runID = "RID"
	c := &Case{
		Name: "x",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
		Steps: []Action{
			{Name: "call", API: &APIStep{
				Request: "GET /x",
				Expect:  []string{"status_code == 200"},
			}},
		},
		Assertions: []Action{
			{Name: "never", Expect: []string{"{{missing_var}} == x"}},
		},
		Cleanup: []Action{
			{Name: "noop", Expect: []string{"a == a"}},
		},
	}
	cr := r.RunCase(context.Background(), c)
	if cr.Verdict != "FAIL" || cr.Failure.Step != "call" {
		t.Fatalf("verdict=%s failure=%+v", cr.Verdict, cr.Failure)
	}
	if len(cr.Assertions) != 0 {
		t.Fatalf("assertions should be skipped on steps failure")
	}
	if len(cr.Cleanup) != 1 || cr.Cleanup[0].Status != "OK" {
		t.Fatalf("cleanup must still run: %+v", cr.Cleanup)
	}
}

func TestRunCase_SetupFailSkipsSteps(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"dbcli targets": []byte(`{"data":[{"name":"orders_uat","adapter":"mysql"}]}`),
		"dbcli mysql":   []byte(`{"data":{"columns":["id"],"rows":[]}}`),
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "x",
		DBs:  map[string]string{"main": "orders_uat"},
		Setup: []Action{
			{Name: "seed", DB: &DBStep{
				SQL:    "SELECT id FROM orders",
				Expect: []string{"count == 1"}, // 0 rows -> fail
			}},
		},
		Steps: []Action{
			{Name: "never_reached", Expect: []string{"a == a"}},
		},
	}
	cr := r.RunCase(context.Background(), c)
	if cr.Verdict != "FAIL" {
		t.Fatalf("setup failure should propagate: verdict=%s", cr.Verdict)
	}
	if len(cr.Steps) != 0 {
		t.Fatal("steps must be skipped when setup fails")
	}
}

func TestRunCase_StandaloneAssert(t *testing.T) {
	f := &fakeRunner{}
	r := newTestRunner(f)
	r.runID = "RID"
	c := &Case{
		Name: "assert_only",
		Assertions: []Action{
			{Name: "check_literal", Expect: []string{"hello == hello", "world != earth"}},
		},
	}
	cr := r.RunCase(context.Background(), c)
	if cr.Verdict != "PASS" {
		t.Fatalf("standalone assertions should pass: %+v", cr)
	}
}
