package aliyunsls

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type adapter struct{}

// New returns the SLS adapter.
func New() logcli.Adapter { return adapter{} }

func (adapter) Name() string { return "sls" }

// Run dispatches the verb: search/trace/doctor → Batch, tail → Stream.
// Flags are parsed from the args after the verb (--key value | --key=value |
// bare --key → "true").
func (a adapter) Run(ctx context.Context, in logcli.Input, out logcli.Output) error {
	cfg, err := ParseConfig(in.Target.Raw)
	if err != nil {
		return err
	}
	raw, err := cred.Read(cfg.Credential)
	if err != nil {
		return err
	}
	c, err := ParseCredential(raw)
	if err != nil {
		return err
	}
	client := NewClient(cfg.Endpoint, cfg.Project, c, cfg.NoProxy)

	verb, rest := splitVerbAndArgs(in.Args)
	// arg is the verb's primary subject as a positional: the query for
	// search/tail, the trace id for trace. The verb already names the
	// dimension, so a `--query`/`--trace-id` flag would just repeat it.
	arg, flags := parseArgs(rest)

	switch verb {
	case "doctor":
		return doDoctor(ctx, client, cfg, out)
	case "search":
		return doSearch(ctx, client, cfg, arg, flags, out)
	case "trace":
		return doTrace(ctx, client, cfg, arg, flags, out)
	case "tail":
		return doTail(ctx, client, cfg, arg, flags, out)
	}
	return errs.Config("SLS_BAD_VERB", "sls supports: search, trace, tail, doctor")
}

var slsVerbs = map[string]bool{
	"search": true,
	"trace":  true,
	"tail":   true,
	"doctor": true,
}

var slsBooleanFlags = map[string]bool{
	"reverse": true,
}

// splitVerbAndArgs keeps the documented form (`search ... --from ...`) while
// also accepting adapter flags before the verb (`--from ... search ...`). The
// latter is natural when a caller thinks of the time window as applying to the
// SLS adapter rather than to a specific verb.
func splitVerbAndArgs(args []string) (string, []string) {
	if len(args) == 0 {
		return "search", nil
	}
	if slsVerbs[args[0]] {
		return args[0], args[1:]
	}
	if strings.HasPrefix(args[0], "--") {
		if idx := firstVerbAfterLeadingFlags(args); idx >= 0 {
			rest := make([]string, 0, len(args)-1)
			rest = append(rest, args[:idx]...)
			rest = append(rest, args[idx+1:]...)
			return args[idx], rest
		}
	}
	return args[0], args[1:]
}

func firstVerbAfterLeadingFlags(args []string) int {
	for i := 0; i < len(args); {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			if slsVerbs[a] {
				return i
			}
			return -1
		}
		key := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			key = key[:eq]
			i++
			continue
		}
		i++
		if slsBooleanFlags[key] {
			if i < len(args) && (args[i] == "true" || args[i] == "false") {
				i++
			}
			continue
		}
		if i < len(args) && !strings.HasPrefix(args[i], "--") {
			i++
		}
	}
	return -1
}

// check is one doctor stage. The doctor envelope `data` is a flat list of these,
// matching jcli/dbcli; overall health is the exit code, not a top-level field.
type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// doDoctor reports a staged health check of the resolved env: config → connect.
// `config` is always ok here (ParseConfig + credential load already passed in Run
// before dispatch); its detail echoes the resolved targets for a human to
// eyeball. `connect` issues the same signed GetLogs that search/trace use against
// the configured logstore (tiny window, size 1), so success proves endpoint +
// project + logstore + cred + RAM permission all line up at once.
//
// The checks envelope is emitted even on failure (the failed stage carries the
// error); the returned error only carries the non-zero exit code — logcli's
// finalize won't double-write over the batch.
func doDoctor(ctx context.Context, c *Client, cfg *SLSConfig, out logcli.Output) error {
	checks := []any{check{
		Name:   "config",
		OK:     true,
		Detail: "project " + cfg.Project + " / logstore " + cfg.Logstore + " @ " + cfg.Endpoint,
	}}
	fromTS, _ := ParseTime("5m")
	toTS, _ := ParseTime("now")
	if _, err := c.Search(ctx, cfg.Logstore, "*", fromTS, toTS, 1, false); err != nil {
		e := errs.From(err)
		checks = append(checks, check{Name: "connect", OK: false, Detail: e.Code + ": " + e.Message})
		_ = out.Batch(checks)
		return err
	}
	checks = append(checks, check{Name: "connect", OK: true, Detail: "GetLogs ok"})
	return out.Batch(checks)
}

func doSearch(ctx context.Context, c *Client, cfg *SLSConfig, query string, flags map[string]string, out logcli.Output) error {
	if query == "" {
		query = "*"
	}
	fromTS, err := ParseTime(flagOr(flags, "from", "1h"))
	if err != nil {
		return err
	}
	toTS, err := ParseTime(flagOr(flags, "to", "now"))
	if err != nil {
		return err
	}
	reverse := true
	if v, ok := flags["reverse"]; ok {
		reverse = v != "false"
	}
	res, err := c.Search(ctx, cfg.Logstore, query, fromTS, toTS, intOr(flags, "size", 100), reverse)
	if err != nil {
		return err
	}
	return emitResult(out, res)
}

func doTrace(ctx context.Context, c *Client, cfg *SLSConfig, traceID string, flags map[string]string, out logcli.Output) error {
	if traceID == "" {
		return errs.General("TRACE_ID_REQUIRED", "trace requires a trace id: logcli <env> trace <trace-id>")
	}
	fromTS, err := ParseTime(flagOr(flags, "from", "24h"))
	if err != nil {
		return err
	}
	toTS, err := ParseTime(flagOr(flags, "to", "now"))
	if err != nil {
		return err
	}
	res, err := c.Trace(ctx, cfg.Logstore, cfg.TraceField, traceID, fromTS, toTS, intOr(flags, "size", 500))
	if err != nil {
		return err
	}
	return emitResult(out, res)
}

// emitResult batches the rows, attaching a warning when SLS reported the scan
// as Incomplete — otherwise a partial result is indistinguishable from a
// complete one and the AI would treat it as "all matching logs".
func emitResult(out logcli.Output, res *LogResult) error {
	recs := logsToAny(res)
	if res != nil && res.Progress == "Incomplete" {
		return out.Batch(recs, "result incomplete: SLS scan did not finish within the retry budget; results may be partial — narrow the time window or query")
	}
	return out.Batch(recs)
}

func doTail(ctx context.Context, c *Client, cfg *SLSConfig, query string, flags map[string]string, out logcli.Output) error {
	if query == "" {
		query = "*"
	}
	interval := 5 * time.Second
	if v, ok := flags["interval"]; ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	s := out.Stream()
	return c.Tail(ctx, cfg.Logstore, query, interval, func(rec map[string]interface{}) error {
		return s.Record(rec)
	})
}

func logsToAny(res *LogResult) []any {
	if res == nil {
		return []any{}
	}
	recs := make([]any, 0, len(res.Logs))
	for _, l := range res.Logs {
		recs = append(recs, l)
	}
	return recs
}

// parseArgs splits ["level: ERROR","--from","1h","--size","100"] into the
// positional subject ("level: ERROR") and the modifier flags. The first bare
// token (one not consumed as a flag's value) is the positional. A flag whose
// next token is another --flag (or end of args) becomes "true".
//
// Only --reverse is boolean, so put the positional first to avoid it being
// swallowed as `--reverse <positional>`.
func parseArgs(args []string) (string, map[string]string) {
	pos := ""
	m := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			if pos == "" {
				pos = a
			}
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			m[key[:eq]] = key[eq+1:]
			continue
		}
		if slsBooleanFlags[key] {
			m[key] = "true"
			if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
				m[key] = args[i+1]
				i++
			}
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			m[key] = args[i+1]
			i++
		} else {
			m[key] = "true"
		}
	}
	return pos, m
}

func flagOr(flags map[string]string, key, def string) string {
	if v, ok := flags[key]; ok && v != "" {
		return v
	}
	return def
}

// intOr parses a positive-count flag (size). A missing, non-numeric, or
// non-positive value falls back to def — `--size 0`/`--size -5` would otherwise
// loop zero times and return a silently-empty result.
func intOr(flags map[string]string, key string, def int) int {
	if v, ok := flags[key]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return def
}
