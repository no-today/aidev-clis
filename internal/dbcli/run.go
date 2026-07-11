package dbcli

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
	Driver     string
	Target     string
	Database   string
	AllowWrite bool
	Args       []string // raw SQL or a verb
	Out        io.Writer
	Pretty     bool
	Raw        bool
}

// Run executes one dbcli invocation and returns the process exit code. Errors go
// to Out (AI-first single stream); exit code co-signals. EVERY invocation is
// audited via Begin/Finish EXACTLY once on the terminal line (writes additionally
// get a "started" line for kill-safety), including ones rejected before dispatch.
func Run(ctx context.Context, reg *Registry, a RunArgs) int {
	cmd := audit.CommandLine(os.Args)

	// earlyErr audits one terminal line (no started, no result) for a pre-dispatch
	// rejection, then renders the error. targetName is the flag value here since a
	// target may not have resolved yet.
	earlyErr := func(targetName string, e *errs.Error) int {
		audit.Begin(audit.Record{Tool: "dbcli", Backend: a.Driver, Target: targetName,
			Command: cmd, AllowWrite: a.AllowWrite}).Finish(e, nil)
		return fail(ctx, a.Out, e, a.Pretty, a.Raw)
	}

	drv, ok := reg.Get(a.Driver)
	if !ok {
		return earlyErr(a.Target, errs.Config("UNKNOWN_DRIVER", unknownDriverMsg(reg, a.Driver)))
	}
	target, err := config.ResolveForAdapter("dbcli", a.Target, a.Driver)
	if err != nil {
		return earlyErr(a.Target, errs.From(err))
	}
	diag.From(ctx).Logf(1, "resolved target=%s adapter=%s", target.Name, target.Adapter)
	if target.Adapter != a.Driver {
		return earlyErr(target.Name, errs.Config("ADAPTER_MISMATCH",
			"target "+target.Name+" is adapter "+target.Adapter+", not "+a.Driver))
	}

	in := Input{Target: target, Args: a.Args, Database: a.Database, AllowWrite: a.AllowWrite}
	diag.From(ctx).Logf(1, "dispatch driver=%s database=%s allow_write=%v", a.Driver, a.Database, a.AllowWrite)
	diag.From(ctx).Logf(2, "args=%q", a.Args)

	// `doctor` is a staged health report (config → [ssh] → connect), reshaped from
	// the driver's normal connect+ping by error code — no per-driver changes. It's
	// a read; audit one terminal line (no started, no result) and return.
	if len(a.Args) > 0 && strings.EqualFold(a.Args[0], "doctor") {
		code, checks, dErr := runDoctor(ctx, drv, target, in)
		op := audit.Begin(audit.Record{Tool: "dbcli", Backend: a.Driver, Target: target.Name,
			Command: cmd, AllowWrite: a.AllowWrite})
		if code == 0 {
			op.Finish(nil, nil)
		} else {
			// dErr carries the real stage code (SSH_*/connect); fall back to a
			// generic code only if a failure somehow arrived without one. The error
			// is a code carrier for the audit line only; the exit code is `code`
			// from runDoctor, NOT dErr.Exit. (Check the pointer, not an interface, to
			// avoid the typed-nil trap.)
			e := dErr
			if e == nil {
				e = errs.General("DOCTOR_UNHEALTHY", "doctor reported failures")
			}
			op.Finish(e, nil)
		}
		_ = emitData(ctx, a.Out, checks, nil, a.Pretty, a.Raw)
		return code
	}

	out := &runOutput{ctx: ctx, w: a.Out, pretty: a.Pretty, raw: a.Raw}
	// Open the op right before dispatch: a "started" line (writes only) then means
	// "actually issued to the adapter" (kill-safety semantics).
	op := audit.Begin(audit.Record{
		Tool: "dbcli", Backend: a.Driver, Target: target.Name,
		Command: cmd, AllowWrite: a.AllowWrite, SideEffecting: a.AllowWrite,
	})
	runErr := safeRun(ctx, drv, in, out)
	op.Finish(runErr, out.resultSummary)

	if runErr != nil {
		e := errs.From(runErr)
		if !out.wrote {
			_ = emitErr(ctx, a.Out, e.Code, e.Message, a.Pretty, a.Raw)
		}
		return e.Exit
	}
	if !out.wrote {
		if !a.Raw {
			_ = envelope.WriteDataCtx(ctx, a.Out, []any{}, nil, a.Pretty) // driver produced nothing
		}
	}
	return 0
}

// safeRun dispatches to the driver with a recover() so a driver panic (e.g. a
// bad arg-index) becomes a normal *errs.Error instead of aborting the process
// with a Go stacktrace before op.Finish runs. Scoped narrowly to the single
// driver call: flag parsing and audit bookkeeping stay outside it, and the
// returned error flows through the existing op.Finish + {error}-envelope path so
// the audit terminal line is still written and the exit code is correct.
func safeRun(ctx context.Context, drv Driver, in Input, out Output) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.General("DRIVER_PANIC", fmt.Sprintf("driver %s panicked: %v", drv.Name(), r))
		}
	}()
	return drv.Run(ctx, in, out)
}

// runOutput implements Output.Batch → envelope, guarding against double emit.
// It carries ctx so the final emit can drain any -v/-vv diagnostics.
type runOutput struct {
	ctx           context.Context
	w             io.Writer
	pretty        bool
	raw           bool
	wrote         bool
	resultSummary map[string]any // {rows:N} for reads, {affected:N}(+matched) for writes
}

func (o *runOutput) Batch(payload any, warnings ...string) error {
	if o.wrote {
		return errs.General("OUTPUT_MISUSE", "Batch called twice")
	}
	o.wrote = true
	// Classify the audit result by payload TYPE (the write convention only): reads
	// are Result, writes are a {affected:N}(+matched) map. Anything else (e.g. the
	// Raw insert stream) leaves resultSummary nil → no `result` in the audit line.
	switch v := payload.(type) {
	case Result:
		o.resultSummary = map[string]any{"rows": len(v.Rows)}
	case *Result:
		o.resultSummary = map[string]any{"rows": len(v.Rows)}
	case map[string]any:
		if aff, ok := v["affected"]; ok {
			o.resultSummary = map[string]any{"affected": aff}
			if m, ok := v["matched"]; ok {
				o.resultSummary["matched"] = m
			}
		}
	}
	return emitData(o.ctx, o.w, payload, warnings, o.pretty, o.raw)
}

// Raw streams pre-formatted text (the insert verb's INSERT statements). Unlike
// Batch it has no double-emit guard: it is meant to be called once per row.
func (o *runOutput) Raw(s string) error {
	o.wrote = true
	_, err := io.WriteString(o.w, s)
	return err
}

// unknownDriverMsg explains an unknown first token. The removed v1 subcommands
// and `version` land here too — without the special cases an agent on muscle
// memory gets a baffling `no db driver query` and retries blind (observed in
// the wild) instead of being told the current form.
func unknownDriverMsg(reg *Registry, token string) string {
	drivers := strings.Join(reg.Names(), ", ")
	switch token {
	case "query", "exec", "schema", "tables", "envs", "describe":
		return fmt.Sprintf("the %q subcommand was removed — the form is driver-first: dbcli <driver> [--target <name>] %q (drivers: %s)",
			token, "<sql>", drivers)
	case "version":
		return "no `version` subcommand; use dbcli --version"
	}
	return fmt.Sprintf("no db driver %q (drivers: %s)", token, drivers)
}

func fail(ctx context.Context, w io.Writer, e *errs.Error, pretty, raw bool) int {
	_ = emitErr(ctx, w, e.Code, e.Message, pretty, raw)
	return e.Exit
}

// emitData writes payload as the envelope, or as raw text when raw.
// In raw mode the envelope is bypassed, so -v/-vv diagnostics (drained by WriteDataCtx via ctx) are intentionally dropped — raw is for pipes.
func emitData(ctx context.Context, w io.Writer, payload any, warnings []string, pretty, raw bool) error {
	if raw {
		return renderRaw(w, payload)
	}
	return envelope.WriteDataCtx(ctx, w, payload, warnings, pretty)
}

// emitErr writes an error as the envelope, or as an "ERROR code: msg" line when raw.
func emitErr(ctx context.Context, w io.Writer, code, msg string, pretty, raw bool) error {
	if raw {
		return envelope.WriteRawError(w, code, msg)
	}
	return envelope.WriteErrorCtx(ctx, w, code, msg, pretty)
}

// dcheck is one doctor stage; the doctor envelope `data` is a flat list of these
// (overall health is the exit code, not a redundant top-level field).
type dcheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type discardOutput struct{}

func (discardOutput) Batch(any, ...string) error { return nil }

// runDoctor runs the driver's normal connect+ping (its `doctor` verb) and
// classifies the outcome into staged checks: a failure with an `SSH_*` code is
// the ssh (bastion) stage; anything else is the connect (db) stage. sqlite has no
// `ssh:` block, so it simply reports config → connect. On failure it also returns
// the classified *errs.Error (real stage code) so the caller can audit it with
// fidelity; on success the error is nil.
func runDoctor(ctx context.Context, drv Driver, target config.Target, in Input) (int, []dcheck, *errs.Error) {
	_, sshConfigured := target.Raw["ssh"].(map[string]any)
	checks := []dcheck{{Name: "config", OK: true, Detail: "adapter " + target.Adapter}}

	err := drv.Run(ctx, in, discardOutput{})
	if err == nil {
		if sshConfigured {
			checks = append(checks, dcheck{Name: "ssh", OK: true, Detail: "tunnel ok"})
		}
		return 0, append(checks, dcheck{Name: "connect", OK: true, Detail: "ping ok"}), nil
	}
	e := errs.From(err)
	if sshConfigured && strings.HasPrefix(e.Code, "SSH_") {
		return e.Exit, append(checks, dcheck{Name: "ssh", OK: false, Detail: e.Code + ": " + e.Message}), e
	}
	if sshConfigured {
		checks = append(checks, dcheck{Name: "ssh", OK: true, Detail: "tunnel ok"})
	}
	return e.Exit, append(checks, dcheck{Name: "connect", OK: false, Detail: e.Code + ": " + e.Message}), e
}
