package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/buildinfo"
	"github.com/no-today/aidev-clis/internal/core/clihelp"
	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/logcli"
)

const usage = `logcli <adapter> [--target <name>] [--fields <f1,f2>] [--timeout <dur>] [--config-dir <path>] [--pretty] [-v|-vv] <args...>
       logcli targets              # list configured targets (name, adapter, description)

--fields projects each structured record down to the named fields (e.g.
--fields message,__time__ strips SLS __tag__:*/container metadata); plain-line
records pass through untouched.

Read logs from many sources (least-privilege, credential-hiding).

Adapters: local-file, kubectl, docker, ssh-file, ssh-docker, sls
  logcli sls --target app_prod search "level: ERROR" --from 1h --to now
  logcli kubectl --target uat_k8s logs -l app=foo --tail 200

Output is the {data}|{error} JSON / NDJSON envelope (see docs/OUTPUT-CONTRACT.md);
--output raw drops the envelope (one verbatim log line per record).

Diagnostics: -v / -vv (single dash, stackable; or --verbose) add a "diagnostics"
array to the envelope (on the NDJSON "end"/"error" line when streaming). -v = target
resolution, dispatch, record count; -vv = also the adapter args. Omitted entirely
without -v, so default output is unchanged. (--v with two dashes is NOT a flag.)`

// cliFlags are the logcli-level flags peeled off before the adapter's own args.
type cliFlags struct {
	adapter   string
	target    string
	configDir string
	output    string
	timeout   string
	pretty    bool
	verbose   int
	fields    string   // comma-separated record-field projection ("" = full records)
	rest      []string // pass-through args for the adapter
	badFlag   string   // a logcli value-flag given with no value (e.g. `--target` at end)
	legacyEnv bool     // legacy --env given (renamed to --target)
}

// writeCmdErr emits a cmd-layer error: a plain "ERROR code: msg" line in raw
// mode (fully un-enveloped), otherwise the {error} envelope.
func writeCmdErr(w io.Writer, raw, pretty bool, code, msg string) {
	if raw {
		_ = envelope.WriteRawError(w, code, msg)
		return
	}
	_ = envelope.WriteError(w, code, msg, pretty)
}

func main() {
	reg := logcli.NewRegistry(builtins())

	root := &cobra.Command{
		Use:                "logcli <adapter> [--target n] <args...>",
		Short:              "Read logs from many sources (least-privilege, credential-hiding)",
		SilenceUsage:       true,
		SilenceErrors:      true,
		DisableFlagParsing: true, // forward native flags (e.g. -f) to the adapter
		ValidArgsFunction: clihelp.ArgvCompleter{
			Tool:       "logcli",
			Known:      reg.Names(),
			Canonical:  reg.Canonical,
			VerbsFor:   adapterVerbs,
			ValueFlags: []string{"--target", "--fields", "--config-dir", "--output", "--timeout", "--from", "--to", "--size", "--interval"},
			FlagsFor:   logcliFlagsFor,
		}.Complete,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --help / -h short-circuit (DisableFlagParsing means cobra won't).
			for _, a := range args {
				if a == "--help" || a == "-h" {
					fmt.Fprintln(os.Stdout, usage)
					os.Exit(0)
				}
				if a == "--version" {
					fmt.Fprintln(os.Stdout, "logcli version "+buildinfo.Version)
					os.Exit(0)
				}
			}
			f := parseArgs(args)
			raw := f.output == "raw"

			// --config-dir must take effect before ANY audit call (and any
			// config/credential load): audit.writeLine resolves config.Home(),
			// which reads AIDEV_CLIS_HOME — so set it first or early-error lines
			// (BAD_FLAG etc.) get misfiled to the default dir.
			if f.configDir != "" {
				_ = os.Setenv("AIDEV_CLIS_HOME", f.configDir)
			}

			if f.legacyEnv {
				msg := "--env was renamed to --target; pass --target <name> (see `logcli targets`)"
				audit.Begin(audit.Record{
					Tool: "logcli", Backend: f.adapter, Command: audit.CommandLine(os.Args),
				}).Finish(errs.Config("LEGACY_FLAG", msg), nil)
				writeCmdErr(os.Stdout, raw, f.pretty, "LEGACY_FLAG", msg)
				os.Exit(2) // ExitConfig
			}
			if f.badFlag != "" {
				audit.Begin(audit.Record{
					Tool: "logcli", Backend: f.adapter, Command: audit.CommandLine(os.Args),
				}).Finish(errs.Config("BAD_FLAG", fmt.Sprintf("%s requires a value", f.badFlag)), nil)
				writeCmdErr(os.Stdout, raw, f.pretty, "BAD_FLAG",
					fmt.Sprintf("%s requires a value", f.badFlag))
				os.Exit(2) // ExitConfig
			}

			if f.output != "" && f.output != "json" && f.output != "raw" {
				audit.Begin(audit.Record{
					Tool: "logcli", Backend: f.adapter, Command: audit.CommandLine(os.Args),
				}).Finish(errs.Config("UNSUPPORTED_OUTPUT", "logcli supports --output json|raw"), nil)
				writeCmdErr(os.Stdout, raw, f.pretty, "UNSUPPORTED_OUTPUT",
					"logcli supports --output json|raw")
				os.Exit(2) // ExitConfig
			}
			// `targets` is a reserved discovery verb (no --target, no backend call):
			// list the configured targets so an agent can see what's available.
			if f.adapter == "targets" {
				infos, err := config.ListTargets("logcli")
				var targetsErr error
				if err != nil {
					targetsErr = errs.From(err)
				}
				audit.Begin(audit.Record{
					Tool: "logcli", Command: audit.CommandLine(os.Args),
				}).Finish(targetsErr, nil)
				if err != nil {
					e := errs.From(err)
					writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
					os.Exit(e.Exit)
				}
				tctx := context.Background()
				if f.verbose > 0 {
					tctx = diag.With(tctx, diag.New(f.verbose))
				}
				_ = emitTargets(tctx, os.Stdout, infos, raw, f.pretty)
				os.Exit(0)
			}
			if f.adapter == "" {
				// Still an invocation: audit it so there is no silent path.
				audit.Begin(audit.Record{
					Tool: "logcli", Command: audit.CommandLine(os.Args),
				}).Finish(errs.Config("NO_ADAPTER", "missing <adapter>"), nil)
				writeCmdErr(os.Stdout, raw, f.pretty, "NO_ADAPTER",
					"missing <adapter>; "+strings.SplitN(usage, "\n", 2)[0])
				os.Exit(2) // ExitConfig
			}

			ctx, stop := clihelp.SignalContext()
			defer stop()
			if f.verbose > 0 {
				ctx = diag.With(ctx, diag.New(f.verbose))
			}
			if f.timeout != "" {
				d, err := time.ParseDuration(f.timeout)
				if err != nil {
					msg := fmt.Sprintf("invalid --timeout %q: %v", f.timeout, err)
					audit.Begin(audit.Record{
						Tool: "logcli", Backend: f.adapter, Command: audit.CommandLine(os.Args),
					}).Finish(errs.Config("BAD_TIMEOUT", msg), nil)
					writeCmdErr(os.Stdout, raw, f.pretty, "BAD_TIMEOUT", msg)
					os.Exit(2)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}

			code := logcli.Run(ctx, reg, logcli.RunArgs{
				Adapter: f.adapter, Target: f.target, Args: f.rest, Fields: f.fields,
				Out: os.Stdout, ErrOut: os.Stderr, Pretty: f.pretty, Raw: raw,
			})
			os.Exit(code)
			return nil
		},
	}
	_ = root.Execute()
}

// parseArgs peels the logcli-level flags (adapter, --target, --config-dir,
// --output, --timeout, --pretty) and returns the remaining pass-through args
// for the adapter. Both `--flag value` and `--flag=value` forms are accepted;
// a value-flag consumes its following token even when that token starts with
// '-' (e.g. `--target -x`, a dash-prefixed identifier).
func parseArgs(args []string) cliFlags {
	var f cliFlags
	takeVal := func(i *int, name string) (string, bool) {
		a := args[*i]
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
		// A value-flag consumes the next token as its value even when that token
		// starts with '-'. Only value-flags reach takeVal; boolean/count flags are
		// handled in their own switch cases and never consume a token.
		if a == name && *i+1 < len(args) {
			*i++
			return args[*i], true
		}
		return "", false
	}
	// take records a BAD_FLAG when a value-flag is given with no value (e.g.
	// `--target` at end) instead of silently defaulting — for a targeting flag, a
	// silent default could hit the wrong target.
	take := func(f *cliFlags, i *int, name string) string {
		v, ok := takeVal(i, name)
		if !ok {
			f.badFlag = name
		}
		return v
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--pretty":
			f.pretty = true
		case strings.HasPrefix(a, "--pretty="):
			f.pretty = flagBool(strings.TrimPrefix(a, "--pretty="))
		case a == "--verbose":
			f.verbose++
		case strings.HasPrefix(a, "--verbose="):
			f.verbose += flagVerbose(strings.TrimPrefix(a, "--verbose="))
		case isShortVerbose(a):
			f.verbose += len(a) - 1 // -v=1, -vv=2, ...
		case matches(a, "--target"):
			f.target = take(&f, &i, "--target")
		// Legacy selector: --env was renamed to --target (2026-07). Without this
		// case it would fall through to f.rest as an adapter arg, be silently
		// ignored, and the query would run against default_target — returning
		// plausible data from the WRONG target (observed in the wild: half a
		// session of wrong-logstore diagnosis). Reject loudly instead.
		case matches(a, "--env"):
			f.legacyEnv = true
			take(&f, &i, "--env") // consume its value so it doesn't leak into rest
			f.badFlag = ""        // BAD_FLAG is moot; LEGACY_FLAG supersedes
		case matches(a, "--fields"):
			f.fields = take(&f, &i, "--fields")
		case matches(a, "--config-dir"):
			f.configDir = take(&f, &i, "--config-dir")
		case matches(a, "--output"):
			f.output = take(&f, &i, "--output")
		case matches(a, "--timeout"):
			f.timeout = take(&f, &i, "--timeout")
		case f.adapter == "" && len(a) > 0 && a[0] != '-':
			f.adapter = a
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f
}

// matches reports whether arg is `name` or `name=...`.
func matches(arg, name string) bool {
	return arg == name || strings.HasPrefix(arg, name+"=")
}

// emitTargets writes the `targets` listing: in raw mode one target name per
// line, otherwise the {data} envelope via the Ctx writer so `targets -v` carries
// diagnostics like every other JSON path (the non-Ctx WriteData silently drops
// them).
func emitTargets(ctx context.Context, w io.Writer, infos []config.TargetInfo, raw, pretty bool) error {
	if raw {
		for _, info := range infos {
			fmt.Fprintln(w, info.Name)
		}
		return nil
	}
	diag.From(ctx).Logf(1, "listed %d logcli targets", len(infos))
	return envelope.WriteDataCtx(ctx, w, infos, nil, pretty)
}

// flagBool parses the value of a `--flag=value` boolean form; a missing or
// unparseable value counts as true (the flag was explicitly given).
func flagBool(v string) bool {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}

// flagVerbose parses the value of `--verbose=N`; an unparseable value counts as
// one level.
func flagVerbose(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 1
	}
	return n
}

// isShortVerbose reports whether arg is -v, -vv, -vvv, ... (a count flag).
func isShortVerbose(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	return strings.Trim(arg[1:], "v") == ""
}
