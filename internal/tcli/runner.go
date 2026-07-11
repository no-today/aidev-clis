package tcli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Runner executes cases. cli runs child CLIs, disc discovers env adapters,
// tmpDir holds inline-actor temp files, runID seeds {{run_id}} and the report.
type Runner struct {
	cli    CLIRunner
	disc   *discoverer
	env    []string // AIDEV_CLIS_HOME passthrough
	tmpDir string
	runID  string
}

func newRunID() string { return "tcli-" + uuid.NewString()[:8] }

// NewRunner builds a production Runner (localCLI + sibling resolution + config passthrough).
func NewRunner(configDir string) *Runner {
	var env []string
	if configDir != "" {
		env = append(env, "AIDEV_CLIS_HOME="+configDir)
	}
	cli := localCLI{}
	return &Runner{cli: cli, disc: newDiscoverer(cli, env), env: env, runID: newRunID()}
}

// RunCase runs the four phases: setup failure skips steps/assertions; steps
// failure skips assertions; cleanup always runs (best-effort). vars are shared.
func (r *Runner) RunCase(ctx context.Context, c *Case) CaseResult {
	vars := map[string]string{}
	for k, v := range c.Vars {
		vars[k] = v
	}
	seedBuiltins(vars, r.runID)

	phases := map[string][]StepResult{}
	var first *Failure
	run := func(phase string, acts []Action, stopOnFail bool) {
		res, f := r.runPhase(ctx, c, phase, acts, vars, stopOnFail)
		phases[phase] = res
		if f != nil && first == nil {
			first = f
		}
	}
	run("setup", c.Setup, true)
	if first == nil {
		run("steps", c.Steps, true)
	}
	if first == nil {
		run("assertions", c.Assertions, true)
	}
	cres, _ := r.runPhase(ctx, c, "cleanup", c.Cleanup, vars, false)
	phases["cleanup"] = cres

	return BuildCaseResult(c, r.runID, phases, first)
}

func (r *Runner) runPhase(ctx context.Context, c *Case, phase string, acts []Action, vars map[string]string, stopOnFail bool) ([]StepResult, *Failure) {
	var out []StepResult
	var first *Failure
	for _, a := range acts {
		sr, f := r.runAction(ctx, c, phase, a, vars)
		out = append(out, sr)
		if f != nil {
			f.Phase, f.Step, f.Type = phase, a.Name, a.actionKind()
			if first == nil {
				first = f
				if stopOnFail {
					break
				}
			}
		}
	}
	return out, first
}

func (r *Runner) runAction(ctx context.Context, c *Case, phase string, a Action, vars map[string]string) (res StepResult, f *Failure) {
	kind := a.actionKind()
	res = StepResult{Name: a.Name, Type: kind}
	start := nowMS()
	defer func() { res.ElapsedMS = nowMS() - start }()

	switch kind {
	case "expect":
		res, f = r.runAssert(phase, a, vars, res)
	case "api":
		res, f = r.runWithRetry(ctx, c, phase, a, vars, res, r.attemptAPI, a.API.Wait, a.API.Expect, a.API.Capture, "body")
	case "db":
		res, f = r.runWithRetry(ctx, c, phase, a, vars, res, r.attemptDB, a.DB.Wait, a.DB.Expect, a.DB.Capture, "rows")
	case "log":
		res, f = r.runLog(ctx, c, phase, a, vars, res)
	default:
		res.Status = "ERROR"
		f = fail(phase, a, "config_invalid", "ACTION_INVALID", "action has no recognized kind")
	}
	return res, f
}

type attemptFn func(ctx context.Context, c *Case, a Action, vars map[string]string, res *StepResult) (cliResult, *Failure)

// runWithRetry runs one attempt + expect + capture, retrying per wait.
func (r *Runner) runWithRetry(ctx context.Context, c *Case, phase string, a Action, vars map[string]string,
	res StepResult, attempt attemptFn, wait *Wait, exprs []string, capture map[string]string, coll string) (StepResult, *Failure) {

	deadline, interval := waitParams(wait)
	loopStart := time.Now()
	for {
		res.Attempts++
		cr, f := attempt(ctx, c, a, vars, &res)
		if f != nil {
			if phase == "cleanup" && f.Code == "TEMPLATE_VAR_MISSING" {
				res.Status = "SKIPPED"
				return res, nil
			}
			if !retryable(f) || time.Since(loopStart) >= deadline || !sleepCtx(ctx, interval) {
				res.Status = stepStatus(f)
				return res, f
			}
			continue
		}
		res.Last = snapshot(cr.Data)
		if err := EvalExprs(cr.Data, exprs, coll, vars); err != nil {
			e := errs.From(err)
			ef := fail(phase, a, "assertion_failed", e.Code, e.Message)
			ef.Evidence = res.Last
			ef.Next = nextActions(c, a)
			if time.Since(loopStart) >= deadline || !sleepCtx(ctx, interval) {
				res.Status = "FAILED"
				return res, ef
			}
			continue
		}
		if err := applyCaptures(cr.Data, capture, vars); err != nil {
			e := errs.From(err)
			res.Status = "FAILED"
			return res, fail(phase, a, "assertion_failed", e.Code, e.Message)
		}
		captureTrace(a, cr.Data, vars)
		if a.API != nil && a.API.Assert != nil {
			if sf := runScriptAssert(ctx, a.API.Assert, vars); sf != nil {
				res.Status = "FAILED"
				sf.Evidence = res.Last
				return res, sf
			}
		}
		res.Status = "OK"
		return res, nil
	}
}

func (r *Runner) attemptAPI(ctx context.Context, c *Case, a Action, vars map[string]string, res *StepResult) (cliResult, *Failure) {
	st := a.API
	app, actor, baseURL, err := resolveAPITarget(c, st)
	if err != nil {
		return cliResult{}, failErr(*res, "config_invalid", err)
	}
	block, err := Render(st.Request, vars)
	if err != nil {
		return cliResult{}, failErr(*res, "template_failed", err)
	}
	method, path, headers, body := parseRawHTTP(block)
	ract, err := r.resolveActorFlags(actor, vars)
	if err != nil {
		return cliResult{}, failErr(*res, "config_invalid", err)
	}
	res.App, res.Actor = app, ract.Name
	args := apiArgs(app, method, path, headers, body, ract, baseURL, st.SaveBody, st.Timeout)
	stdout, runErr := r.cli.Run(ctx, "apicli", args, r.env)
	return r.classify(*res, stdout, runErr)
}

func (r *Runner) attemptDB(ctx context.Context, c *Case, a Action, vars map[string]string, res *StepResult) (cliResult, *Failure) {
	st := a.DB
	target, err := resolveTarget(st.Target, c.DBs, "db")
	if err != nil {
		return cliResult{}, failErr(*res, "config_invalid", err)
	}
	res.Target = target
	if st.Write && !c.Safety.AllowDBWrite {
		return cliResult{}, fail("", a, "safety_blocked", "SAFETY_BLOCKED",
			"db write requires safety.allow_db_write: true")
	}
	sql, err := Render(st.SQL, vars)
	if err != nil {
		return cliResult{}, failErr(*res, "template_failed", err)
	}
	driver, err := r.disc.dbDriver(ctx, target)
	if err != nil {
		return cliResult{}, failErr(*res, "config_invalid", err)
	}
	args := dbArgs(driver, target, sql, st.Write, st.Timeout)
	stdout, runErr := r.cli.Run(ctx, "dbcli", args, r.env)
	return r.classify(*res, stdout, runErr)
}

// runLog handles trace/query: empty/undefined needle -> SKIP; sls server-side;
// non-sls fetch + client grep. Retries per wait. count collection = root array.
func (r *Runner) runLog(ctx context.Context, c *Case, phase string, a Action, vars map[string]string, res StepResult) (StepResult, *Failure) {
	st := a.Log
	raw := st.Trace
	if raw == "" {
		raw = st.Query
	}
	needle, err := Render(raw, vars)
	if err != nil {
		if errs.From(err).Code == "TEMPLATE_VAR_MISSING" {
			res.Status = "SKIPPED" // no correlation key -> don't gate
			return res, nil
		}
		return res, failErr(res, "template_failed", err)
	}
	if strings.TrimSpace(needle) == "" {
		res.Status = "SKIPPED"
		return res, nil
	}
	target, err := resolveTarget(st.Target, c.Logs, "log")
	if err != nil {
		return res, failErr(res, "config_invalid", err)
	}
	res.Target = target
	adapter, err := r.disc.logAdapter(ctx, target)
	if err != nil {
		return res, failErr(res, "config_invalid", err)
	}

	deadline, interval := waitParams(st.Wait)
	loopStart := time.Now()
	args := logArgs(adapter, target, st, needle)
	for {
		res.Attempts++
		stdout, runErr := r.cli.Run(ctx, "logcli", args, r.env)
		cr, f := r.classify(res, stdout, runErr)
		if f != nil {
			if !retryable(f) || time.Since(loopStart) >= deadline || !sleepCtx(ctx, interval) {
				res.Status = stepStatus(f)
				return res, f
			}
			continue
		}
		data := cr.Data
		if adapter != "sls" {
			data = grepRecords(cr.Data, needle)
		}
		res.Last = snapshot(data)
		if err := EvalExprs(data, st.Expect, "", vars); err != nil {
			e := errs.From(err)
			lf := fail(phase, a, "assertion_failed", e.Code, e.Message)
			lf.Evidence = res.Last
			if time.Since(loopStart) >= deadline || !sleepCtx(ctx, interval) {
				res.Status = "FAILED"
				return res, lf
			}
			continue
		}
		if err := applyCaptures(data, st.Capture, vars); err != nil {
			e := errs.From(err)
			res.Status = "FAILED"
			return res, fail(phase, a, "assertion_failed", e.Code, e.Message)
		}
		res.Status = "OK"
		return res, nil
	}
}

// runAssert evaluates a standalone assertion action's expressions over vars
// (no payload — each expression is {{}}-rendered then compared as literals).
func (r *Runner) runAssert(phase string, a Action, vars map[string]string, res StepResult) (StepResult, *Failure) {
	if err := EvalExprs(nil, a.Expect, "", vars); err != nil {
		e := errs.From(err)
		if phase == "cleanup" && e.Code == "TEMPLATE_VAR_MISSING" {
			res.Status = "SKIPPED"
			return res, nil
		}
		cat := "assertion_failed"
		if e.Code == "TEMPLATE_VAR_MISSING" {
			cat = "template_failed"
		}
		res.Status = "FAILED"
		return res, fail(phase, a, cat, e.Code, e.Message)
	}
	res.Status = "OK"
	return res, nil
}

// ————————————————— helpers —————————————————

var nowMS = func() int64 { return time.Now().UnixNano() / 1e6 }

// sleepCtx waits up to d for the next retry, but returns early if ctx is
// cancelled or its deadline expires. Reports false when the wait was cut short
// by ctx (caller should stop retrying and surface the pending failure promptly),
// true when the full interval elapsed. A non-positive d is a no-op wait.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func waitParams(w *Wait) (time.Duration, time.Duration) {
	if w == nil {
		return 0, 0
	}
	d := 30 * time.Second
	i := 2 * time.Second
	if w.Timeout != "" {
		if x, err := time.ParseDuration(w.Timeout); err == nil {
			d = x
		}
	}
	if w.Interval != "" {
		if x, err := time.ParseDuration(w.Interval); err == nil {
			i = x
		}
	}
	return d, i
}

func retryable(f *Failure) bool {
	switch f.Category {
	case "remote_failed", "wait_timeout", "assertion_failed":
		return true
	}
	return false
}

func stepStatus(f *Failure) string {
	if f.Category == "assertion_failed" || f.Category == "safety_blocked" {
		return "FAILED"
	}
	return "ERROR"
}

func (r *Runner) classify(res StepResult, stdout []byte, runErr error) (cliResult, *Failure) {
	cr, err := parseEnvelope(stdout, runErr)
	if err != nil {
		e := errs.From(err)
		return cr, failCode(res, categoryForExit(e.Exit), e.Code, e.Message)
	}
	if !cr.HasData {
		return cr, failCode(res, categoryForCode(cr.ErrCode), cr.ErrCode, cr.ErrMsg)
	}
	return cr, nil
}

func categoryForExit(exit int) string {
	switch exit {
	case errs.ExitConfig:
		return "config_invalid"
	case errs.ExitAuth:
		return "auth_required"
	case errs.ExitTimeout:
		return "wait_timeout"
	case errs.ExitRemote:
		return "remote_failed"
	}
	return "remote_failed"
}

func categoryForCode(code string) string {
	switch {
	case strings.Contains(code, "AUTH") || strings.Contains(code, "SESSION"):
		return "auth_required"
	case strings.Contains(code, "TARGET") || strings.Contains(code, "CONFIG") ||
		strings.Contains(code, "NOT_FOUND") || strings.Contains(code, "WRITE_NOT_ALLOWED"):
		return "config_invalid"
	case strings.Contains(code, "TIMEOUT"):
		return "wait_timeout"
	}
	return "remote_failed"
}

func fail(phase string, a Action, category, code, msg string) *Failure {
	return &Failure{Phase: phase, Step: a.Name, Type: a.actionKind(), Category: category, Code: code, Message: msg}
}
func failCode(res StepResult, category, code, msg string) *Failure {
	return &Failure{Step: res.Name, Type: res.Type, Category: category, Code: code, Message: msg}
}
func failErr(res StepResult, category string, err error) *Failure {
	e := errs.From(err)
	return &Failure{Step: res.Name, Type: res.Type, Category: category, Code: e.Code, Message: e.Message}
}

// applyCaptures extracts {var: path} values from the payload into vars.
func applyCaptures(payload []byte, capture, vars map[string]string) error {
	for v, path := range capture {
		val, ok := Extract(payload, path)
		if !ok {
			return errs.General("CAPTURE_PATH_MISSING", "capture "+v+": path not found: "+path)
		}
		vars[v] = val
	}
	return nil
}

// captureTrace: after an api step, auto-capture trace_id (if non-empty) as a
// zero-config {{trace_id}} default.
func captureTrace(a Action, data []byte, vars map[string]string) {
	if a.API == nil {
		return
	}
	if t := gjson.GetBytes(data, "trace_id"); t.Exists() && t.String() != "" {
		vars["trace_id"] = t.String()
	}
}

// grepRecords: non-sls, keep records whose message contains needle; rebuild the
// array. Zero matches -> "[]" (not json.Marshal(nil)'s "null") so count stays correct.
func grepRecords(data []byte, needle string) []byte {
	var records []json.RawMessage
	_ = json.Unmarshal(data, &records)
	var matched []json.RawMessage
	for _, rec := range records {
		if recordMatches(rec, needle) {
			matched = append(matched, rec)
		}
	}
	if len(matched) == 0 {
		return []byte("[]")
	}
	out, _ := json.Marshal(matched)
	return out
}

// recordMatches reports whether a non-sls log record contains needle. Line
// adapters shape records as {"message": line}, but structured adapters may name
// the text field differently (or omit "message" entirely). We match on
// "message" when the key is present (preserving the precise, field-scoped
// behavior); when it is absent we fall back to the record's raw JSON text so a
// differing field name yields a match instead of a silent miss that would
// otherwise surface as a slow wait_timeout. Conservative: the fallback only
// widens matching for records that lack the expected field.
func recordMatches(rec json.RawMessage, needle string) bool {
	if m := gjson.GetBytes(rec, "message"); m.Exists() {
		return strings.Contains(m.String(), needle)
	}
	return strings.Contains(string(rec), needle)
}
