package logcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// RunArgs are the resolved inputs to the run loop (flags already parsed).
type RunArgs struct {
	Adapter string
	Target  string
	Args    []string // pass-through args after `logcli <adapter>`
	Fields  string   // comma-separated projection of record fields ("" = full records)
	Out     io.Writer
	ErrOut  io.Writer
	Pretty  bool
	Raw     bool
}

// Run executes one logcli invocation and returns the process exit code.
// Errors are written to Out (AI-first single stream); exit code co-signals.
//
// Invariant (docs/SECURITY-MODEL.md): EVERY invocation is audited — including
// ones rejected before adapter dispatch (unknown adapter, config/target errors,
// adapter mismatch). logcli is read-only: no started lines, no id, exactly one
// terminal line per invocation.
func Run(ctx context.Context, reg *Registry, a RunArgs) int {
	cmd := audit.CommandLine(os.Args)

	// earlyErr audits one terminal line for a pre-dispatch rejection and renders
	// the error. targetName is the flag value since a target may not have resolved.
	earlyErr := func(targetName string, e *errs.Error) int {
		audit.Begin(audit.Record{
			Tool: "logcli", Backend: a.Adapter, Target: targetName, Command: cmd,
		}).Finish(e, nil)
		return fail(ctx, a.Out, e, a.Pretty, a.Raw)
	}

	ad, ok := reg.Get(a.Adapter)
	if !ok {
		return earlyErr(a.Target, errs.Config("UNKNOWN_ADAPTER", fmt.Sprintf("no log adapter %q", a.Adapter)))
	}
	target, err := config.ResolveForAdapter("logcli", a.Target, ad.Name())
	if err != nil {
		return earlyErr(a.Target, asErr(err))
	}
	diag.From(ctx).Logf(1, "resolved target=%s adapter=%s", target.Name, target.Adapter)
	// Compare the selected adapter against the target's configured adapter.
	if c := reg.Canonical(target.Adapter); c == "" || c != ad.Name() {
		return earlyErr(target.Name, errs.Config("ADAPTER_MISMATCH",
			fmt.Sprintf("target %q is adapter %q, not %q", target.Name, target.Adapter, a.Adapter)))
	}

	diag.From(ctx).Logf(1, "dispatch adapter=%s", ad.Name())
	diag.From(ctx).Logf(2, "args=%q", a.Args)
	out := &runOutput{ctx: ctx, w: a.Out, pretty: a.Pretty, raw: a.Raw, fields: splitFields(a.Fields)}

	// An implicit default target is how a query silently lands on the wrong
	// logstore (a stale script, a forgotten flag): when more than one target is
	// configured, surface which one actually served the query. Explicit --target
	// stays warning-free. Batch/none modes only; streams have no warnings channel
	// (-v diagnostics carry the resolved target there).
	if a.Target == "" {
		if infos, lerr := config.ListTargets("logcli"); lerr == nil && len(infos) > 1 {
			out.preWarn = []string{fmt.Sprintf(
				"no --target given; used default target %q (%d targets configured — pass --target to be explicit)",
				target.Name, len(infos))}
		}
	}

	// Open the op after successful target resolution so target.Name is canonical.
	// logcli is never side-effecting: no started line, no id.
	var runErr error
	var result map[string]any
	op := audit.Begin(audit.Record{
		Tool: "logcli", Backend: a.Adapter, Target: target.Name, Command: cmd,
	})
	defer func() {
		if out.count > 0 {
			result = map[string]any{"lines": out.count}
		}
		op.Finish(runErr, result)
	}()

	runErr = safeRun(ctx, ad, Input{Target: target, Args: a.Args}, out)
	_ = out.finalize(runErr)
	if runErr != nil {
		return asErr(runErr).Exit
	}
	return 0
}

// runOutput implements Output (and Streamer). It tracks which mode the adapter
// used so finalize can write the terminal envelope.
type outputMode int

const (
	modeNone outputMode = iota
	modeBatch
	modeStream
)

type runOutput struct {
	ctx     context.Context
	w       io.Writer
	pretty  bool
	raw     bool
	mode    outputMode
	stream  *envelope.Stream
	count   int
	fields  []string // record-field projection (nil = full records)
	preWarn []string // run-level warnings prepended to the adapter's own
}

// splitFields parses the --fields value into trimmed, non-empty names.
func splitFields(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// project reduces a map record to the requested fields (missing fields are
// simply absent). Non-map records (plain line adapters) pass through untouched.
func (o *runOutput) project(rec any) any {
	if len(o.fields) == 0 {
		return rec
	}
	m, ok := rec.(map[string]any)
	if !ok {
		return rec
	}
	out := make(map[string]any, len(o.fields))
	for _, f := range o.fields {
		if v, exists := m[f]; exists {
			out[f] = v
		}
	}
	return out
}

// Batch / Stream enforce the adapter contract: EXACTLY ONE of them, once. A
// second output call returns an error rather than writing a second envelope.
func (o *runOutput) Batch(records []any, warnings ...string) error {
	if o.mode != modeNone {
		return errs.General("OUTPUT_MISUSE", "Batch called after output already started")
	}
	o.mode = modeBatch
	o.count = len(records)
	diag.From(o.ctx).Logf(1, "returned %d record(s)", len(records))
	if len(o.fields) > 0 {
		projected := make([]any, len(records))
		for i, rec := range records {
			projected[i] = o.project(rec)
		}
		records = projected
	}
	// raw bypasses the envelope/stream writers, so -v/-vv diagnostics are intentionally dropped (pipe-friendly), same as dbcli.
	if o.raw {
		for _, rec := range records {
			if _, err := fmt.Fprintln(o.w, formatRawRecord(rec)); err != nil {
				return err
			}
		}
		return nil
	}
	return envelope.WriteDataCtx(o.ctx, o.w, records, append(o.preWarn, warnings...), o.pretty)
}

func (o *runOutput) Stream() Streamer {
	if o.mode == modeNone {
		o.mode = modeStream
		o.stream = envelope.NewStream(o.w)
	}
	// If misused (called after Batch, or twice), mode/stream stay as-is; a
	// nil-stream Record would panic, so guard there too.
	return o
}

func (o *runOutput) Record(rec any) error {
	if o.mode != modeStream {
		return errs.General("OUTPUT_MISUSE", "Record called without an active Stream")
	}
	o.count++
	rec = o.project(rec)
	if o.raw {
		_, err := fmt.Fprintln(o.w, formatRawRecord(rec))
		return err
	}
	if o.stream == nil {
		return errs.General("OUTPUT_MISUSE", "Record called without an active Stream")
	}
	return o.stream.Record(rec)
}

// finalize writes the terminal envelope based on mode + runErr; returns the
// audit outcome string.
func (o *runOutput) finalize(runErr error) string {
	if o.raw {
		if runErr != nil {
			e := asErr(runErr)
			_ = envelope.WriteRawError(o.w, e.Code, e.Message)
			return "error"
		}
		return "ok"
	}
	if runErr != nil {
		e := asErr(runErr)
		switch o.mode {
		case modeStream:
			_ = o.stream.ErrCtx(o.ctx, e.Code, e.Message)
		case modeNone:
			_ = envelope.WriteErrorCtx(o.ctx, o.w, e.Code, e.Message, o.pretty)
		}
		// modeBatch: a batch envelope was already written; don't double-write.
		return "error"
	}
	switch o.mode {
	case modeNone:
		_ = envelope.WriteDataCtx(o.ctx, o.w, []any{}, o.preWarn, o.pretty) // adapter produced nothing
	case modeStream:
		diag.From(o.ctx).Logf(1, "streamed %d record(s)", o.count)
		_ = o.stream.EndCtx(o.ctx, o.count)
	}
	return "ok"
}

// safeRun dispatches to the adapter with a recover() so an adapter panic becomes
// a normal *errs.Error instead of crashing the process with a Go stacktrace and
// leaving the deferred op.Finish to write a nil-error terminal line. The returned
// error flows through the existing finalize + op.Finish path, so the audit
// terminal line and {error} envelope are still written with the right exit code.
func safeRun(ctx context.Context, ad Adapter, in Input, out Output) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.General("ADAPTER_PANIC", fmt.Sprintf("adapter %s panicked: %v", ad.Name(), r))
		}
	}()
	return ad.Run(ctx, in, out)
}

func fail(ctx context.Context, w io.Writer, e *errs.Error, pretty, raw bool) int {
	if raw {
		_ = envelope.WriteRawError(w, e.Code, e.Message)
	} else {
		_ = envelope.WriteErrorCtx(ctx, w, e.Code, e.Message, pretty)
	}
	return e.Exit
}

func asErr(err error) *errs.Error { return errs.From(err) }
