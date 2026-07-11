package tcli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshot_RedactsAndTruncates(t *testing.T) {
	in := []byte(`{"body":{"password":"hunter2","token":"abc","note":"` + strings.Repeat("x", 1000) + `"}}`)
	snap := snapshot(in)
	b, _ := json.Marshal(snap)
	s := string(b)
	if strings.Contains(s, "hunter2") || strings.Contains(s, `"abc"`) {
		t.Fatalf("secrets not redacted: %s", s[:80])
	}
	if !strings.Contains(s, "***") {
		t.Fatal("expected *** redaction marker")
	}
	if !strings.Contains(s, "(truncated)") {
		t.Fatal("long value not truncated")
	}
}

func TestBuildCaseResult_Verdicts(t *testing.T) {
	steps := []StepResult{{Name: "a", Status: "OK"}, {Name: "b", Status: "FAILED"}}
	f := &Failure{Phase: "steps", Step: "b", Category: "assertion_failed", Code: "EXPECT_FAILED", Message: "x"}
	cr := BuildCaseResult(&Case{Name: "t", Path: "t.yaml"}, "RID", map[string][]StepResult{"steps": steps}, f)
	if cr.Verdict != "FAIL" || cr.Failure == nil || cr.Counts.Failed != 1 || cr.Counts.Passed != 1 {
		t.Fatalf("verdict/counts wrong: %+v", cr)
	}
	if !strings.Contains(cr.Summary, "FAIL") || !strings.Contains(cr.Summary, "b") {
		t.Fatalf("summary: %q", cr.Summary)
	}
}

func TestBuildCaseResult_SafetyBlocked(t *testing.T) {
	f := &Failure{Category: "safety_blocked", Code: "SAFETY_BLOCKED", Step: "w"}
	cr := BuildCaseResult(&Case{Name: "t"}, "RID", map[string][]StepResult{"steps": {{Status: "FAILED"}}}, f)
	if cr.Verdict != "SAFETY_BLOCKED" {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestVerdictExit(t *testing.T) {
	if verdictExit("PASS") != 0 || verdictExit("FAIL") != 1 || verdictExit("SAFETY_BLOCKED") != 2 {
		t.Fatal("exit mapping wrong")
	}
}

func TestNextActions_APIIncludesActorAndBaseURL(t *testing.T) {
	c := &Case{
		Name: "t",
		Apps: map[string]AppDecl{
			"orders": {App: "orders", Actor: Actor{Name: "qa_buyer"}, BaseURL: "https://uat"},
		},
	}
	a := Action{Name: "create", API: &APIStep{Request: "POST /orders"}}
	na := nextActions(c, a)
	if len(na) != 1 {
		t.Fatalf("want 1 next action, got %d", len(na))
	}
	cmd := na[0].Command
	for _, want := range []string{"apicli call orders /orders", "-X POST", "--actor qa_buyer", "--base-url https://uat", "--output json"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("next command %q missing %q", cmd, want)
		}
	}
}

func TestNextActions_DBIncludesEnv(t *testing.T) {
	c := &Case{
		Name: "t",
		DBs:  map[string]string{"main": "orders_uat"},
	}
	a := Action{Name: "q", DB: &DBStep{SQL: "SELECT 1"}}
	na := nextActions(c, a)
	if len(na) != 1 {
		t.Fatalf("want 1 next action, got %d", len(na))
	}
	if !strings.Contains(na[0].Command, "orders_uat") {
		t.Fatalf("db next action should include env: %q", na[0].Command)
	}
}

func TestBuildSuiteResult_PassAndFail(t *testing.T) {
	cases := []CaseResult{
		{Verdict: "PASS", Case: "a"},
		{Verdict: "FAIL", Case: "b", Summary: "FAIL at steps 'x': mismatch",
			Failure: &Failure{Phase: "steps", Step: "x", Category: "assertion_failed"}},
	}
	sr := BuildSuiteResult("RID", cases)
	if sr.Verdict != "FAIL" || sr.Counts.Total != 2 || sr.Counts.Failed != 1 {
		t.Fatalf("suite counts/verdict: %+v", sr)
	}
	if sr.Failure == nil || sr.Failure.Step != "x" {
		t.Fatalf("suite failure: %+v", sr.Failure)
	}
}
