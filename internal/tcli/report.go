package tcli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ————————————————— 信封 data 形状 —————————————————

type StepResult struct {
	Name      string `json:"name"`
	Type      string `json:"type"`   // api|db|log|equals|exists
	Status    string `json:"status"` // OK|FAILED|ERROR|SKIPPED
	Attempts  int    `json:"attempts,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
	App       string `json:"app,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Target    string `json:"target,omitempty"`
	Last      any    `json:"last,omitempty"` // 最后一次响应精简快照(已脱敏+截断)
}

type Failure struct {
	Phase    string       `json:"phase"`
	Step     string       `json:"step"`
	Type     string       `json:"type"`
	Category string       `json:"category"`
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	Evidence any          `json:"evidence,omitempty"`
	Next     []NextAction `json:"next,omitempty"`
}

type NextAction struct {
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

type Counts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type CaseResult struct {
	Verdict    string       `json:"verdict"` // PASS|FAIL|ERROR|SKIPPED|SAFETY_BLOCKED
	Case       string       `json:"case"`
	Path       string       `json:"path"`
	RunID      string       `json:"run_id"`
	Summary    string       `json:"summary"`
	Failure    *Failure     `json:"failure"`
	Counts     Counts       `json:"counts"`
	Setup      []StepResult `json:"setup"`
	Steps      []StepResult `json:"steps"`
	Assertions []StepResult `json:"assertions"`
	Cleanup    []StepResult `json:"cleanup"`
}

type SuiteResult struct {
	Verdict string       `json:"verdict"`
	Summary string       `json:"summary"`
	RunID   string       `json:"run_id"`
	Counts  Counts       `json:"counts"`            // case 级计数
	Failure *Failure     `json:"failure,omitempty"` // 第一个失败 case 的根因
	Cases   []CaseResult `json:"cases"`
}

const snapMaxStr = 512

// snapshot 把 data 原始 JSON 解析为可读结构,脱敏敏感键、截断长字符串。
func snapshot(data []byte) any {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return truncStr(string(data))
	}
	return redact(v)
}

var sensitive = []string{"password", "passwd", "token", "authorization", "secret", "cookie", "set-cookie"}

func isSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitive(k) {
				out[k] = "***"
				continue
			}
			out[k] = redact(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	case string:
		return truncStr(t)
	default:
		return v
	}
}

func truncStr(s string) string {
	if len(s) <= snapMaxStr {
		return s
	}
	return s[:snapMaxStr] + "...(truncated)"
}

// nextActions 给失败 step 一个可回放命令提示(best-effort)。
func nextActions(c *Case, a Action) []NextAction {
	switch {
	case a.API != nil:
		// 复现命令带上实际解析出的 --actor/--base-url(ENHANCE① 真可回放)。
		app, actor, baseURL, _ := resolveAPITarget(c, a.API)
		method, path, _, _ := parseRawHTTP(a.API.Request)
		if method == "" {
			method = "GET"
		}
		cmd := fmt.Sprintf("apicli call %s %s -X %s", app, path, method)
		reason := "手动调用该 API 看实际响应"
		if lbl := actorLabel(actor); lbl != "" {
			cmd += " --actor " + lbl
			if actor.IsInline() {
				reason += "(inline actor:需自带 --actor-file)"
			}
		}
		if baseURL != "" {
			cmd += " --base-url " + baseURL
		}
		cmd += " --output json"
		return []NextAction{{Command: cmd, Reason: reason}}
	case a.DB != nil:
		target, _ := resolveTarget(a.DB.Target, c.DBs, "db")
		cmd := fmt.Sprintf("dbcli <driver> --target %s %q", target, a.DB.SQL)
		return []NextAction{{Command: cmd, Reason: "手动跑该查询看实际行"}}
	}
	return nil
}

// BuildCaseResult 从各阶段 StepResult + 首个失败组装结论优先的 CaseResult。
func BuildCaseResult(c *Case, runID string, phases map[string][]StepResult, first *Failure) CaseResult {
	cr := CaseResult{
		Case: c.Name, Path: c.Path, RunID: runID, Failure: first,
		Setup: phases["setup"], Steps: phases["steps"],
		Assertions: phases["assertions"], Cleanup: phases["cleanup"],
	}
	for _, ph := range []string{"setup", "steps", "assertions", "cleanup"} {
		for _, s := range phases[ph] {
			cr.Counts.Total++
			switch s.Status {
			case "OK":
				cr.Counts.Passed++
			case "FAILED", "ERROR":
				cr.Counts.Failed++
			case "SKIPPED":
				cr.Counts.Skipped++
			}
		}
	}
	cr.Verdict = verdict(first)
	cr.Summary = summarize(cr.Verdict, first)
	return cr
}

func verdict(f *Failure) string {
	if f == nil {
		return "PASS"
	}
	switch {
	case f.Code == "SAFETY_BLOCKED" || f.Category == "safety_blocked":
		return "SAFETY_BLOCKED"
	case f.Category == "assertion_failed":
		return "FAIL"
	default:
		return "ERROR"
	}
}

func summarize(verdict string, f *Failure) string {
	if f == nil {
		return "PASS"
	}
	return fmt.Sprintf("%s at %s '%s': %s", verdict, f.Phase, f.Step, f.Message)
}

func verdictExit(v string) int {
	switch v {
	case "PASS":
		return 0
	case "SAFETY_BLOCKED":
		return 2
	default: // FAIL, ERROR, SKIPPED
		return 1
	}
}

// BuildSuiteResult 聚合多 case,第一个非 PASS 的根因顶上来。
func BuildSuiteResult(runID string, cases []CaseResult) SuiteResult {
	sr := SuiteResult{RunID: runID, Cases: cases, Verdict: "PASS"}
	for _, c := range cases {
		sr.Counts.Total++
		if c.Verdict == "PASS" {
			sr.Counts.Passed++
			continue
		}
		sr.Counts.Failed++
		if sr.Failure == nil {
			sr.Failure = c.Failure
			sr.Verdict = "FAIL"
			sr.Summary = "FAIL: " + c.Case + " — " + c.Summary
		}
	}
	if sr.Verdict == "PASS" {
		sr.Summary = fmt.Sprintf("PASS: %d/%d cases", sr.Counts.Passed, sr.Counts.Total)
	}
	return sr
}
