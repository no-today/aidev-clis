package tcli

import (
	"context"
	"testing"
)

func newTestRunner(f *fakeRunner) *Runner {
	return &Runner{cli: f, disc: newDiscoverer(f, nil), tmpDir: ""}
}

func TestRunAction_APIPassSavesTrace(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":200,"body":{"id":"ORD-1"},"trace_id":"T-9"}}`),
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "smoke",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
	}
	vars := map[string]string{}
	a := Action{Name: "create", API: &APIStep{
		Request: "POST /orders",
		Expect:  []string{"status_code == 200"},
		Capture: map[string]string{"order_id": "body.id"},
	}}
	res, fail := r.runAction(context.Background(), c, "steps", a, vars)
	if fail != nil || res.Status != "OK" {
		t.Fatalf("status=%s fail=%+v", res.Status, fail)
	}
	if vars["order_id"] != "ORD-1" {
		t.Fatalf("capture failed: %v", vars)
	}
	if vars["trace_id"] != "T-9" {
		t.Fatalf("trace_id not auto-captured: %v", vars)
	}
}

func TestRunAction_APIExpectFail(t *testing.T) {
	f := &fakeRunner{responses: map[string][]byte{
		"apicli call": []byte(`{"data":{"status_code":500,"body":{}}}`),
	}}
	r := newTestRunner(f)
	c := &Case{
		Name: "x",
		Apps: map[string]AppDecl{"orders": {App: "orders"}},
	}
	a := Action{Name: "x", API: &APIStep{
		Request: "GET /x",
		Expect:  []string{"status_code == 200"},
	}}
	res, fail := r.runAction(context.Background(), c, "steps", a, map[string]string{})
	if fail == nil || res.Status != "FAILED" || fail.Category != "assertion_failed" {
		t.Fatalf("want FAILED/assertion_failed, got status=%s fail=%+v", res.Status, fail)
	}
}

func TestRunAction_LogEmptyTraceSkips(t *testing.T) {
	f := &fakeRunner{}
	r := newTestRunner(f)
	c := &Case{
		Name: "x",
		Logs: map[string]string{"sls": "orders_sls"},
	}
	// trace_id not in vars -> Render fails with TEMPLATE_VAR_MISSING -> SKIP
	a := Action{Name: "trace", Log: &LogStep{Trace: "{{trace_id}}"}}
	res, fail := r.runAction(context.Background(), c, "steps", a, map[string]string{})
	if fail != nil || res.Status != "SKIPPED" {
		t.Fatalf("empty needle must SKIP, got status=%s fail=%+v", res.Status, fail)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not call logcli when needle empty: %v", f.calls)
	}
}

func TestRunAction_CleanupMissingVarSkips(t *testing.T) {
	f := &fakeRunner{}
	r := newTestRunner(f)
	c := &Case{
		Name:   "x",
		DBs:    map[string]string{"main": "orders_uat"},
		Safety: Safety{AllowDBWrite: true},
	}
	a := Action{Name: "drop", DB: &DBStep{SQL: "DELETE FROM x WHERE id='{{order_id}}'", Write: true}}
	res, fail := r.runAction(context.Background(), c, "cleanup", a, map[string]string{})
	if fail != nil || res.Status != "SKIPPED" {
		t.Fatalf("cleanup w/ missing var must SKIP, got status=%s fail=%+v", res.Status, fail)
	}
}

func TestRunAction_DBWriteBlocked(t *testing.T) {
	f := &fakeRunner{}
	r := newTestRunner(f)
	// allow_db_write defaults to false
	c := &Case{
		Name: "x",
		DBs:  map[string]string{"main": "orders_uat"},
	}
	a := Action{Name: "w", DB: &DBStep{SQL: "DELETE FROM x", Write: true}}
	res, fail := r.runAction(context.Background(), c, "steps", a, map[string]string{})
	if fail == nil || res.Status != "FAILED" || fail.Code != "SAFETY_BLOCKED" {
		t.Fatalf("want SAFETY_BLOCKED, got status=%s fail=%+v", res.Status, fail)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not call dbcli when blocked: %v", f.calls)
	}
}
