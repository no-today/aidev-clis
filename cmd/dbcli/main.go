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
	"github.com/no-today/aidev-clis/internal/dbcli"
)

const usage = `dbcli <driver> [--target <name>] [--database <db>] [--allow-write] [--timeout <dur>] [--config-dir <path>] [--pretty] [-v|-vv] "<sql>" | --file <q.sql|->
       dbcli <driver> [--target <name>] databases | tables [<db>] | describe [<db>.]<table> | doctor
       dbcli <driver> [--target <name>] insert [--table <name>] "<SELECT ...>"   # rows as INSERT statements (raw SQL)
       dbcli targets

Query databases (least-privilege, credential-hiding). Drivers: mysql, postgres, kingbase, redis, sqlite, mongo, dataease (last-resort read-only HTTP bypass).
Reads default to LIMIT 100; an explicit LIMIT up to 100 is honored, beyond that
errors — paginate with LIMIT 100 OFFSET <n> for more.
Output is the {data}|{error} JSON envelope (see docs/OUTPUT-CONTRACT.md);
--output raw drops the envelope (rows as TSV, no header — pipe to grep/awk/cut).

Diagnostics: -v / -vv (single dash, stackable; or --verbose) add a "diagnostics"
array to the envelope. -v = target resolution, dispatch, connect timing, rows, SSH
tunnel; -vv = also the final SQL (incl. auto-LIMIT) and args. Omitted entirely
without -v, so default output is unchanged. (--v with two dashes is NOT a flag;
raw output from 'insert' carries no diagnostics.)`

// dbcliVerbs returns the second-positional completions for a driver. The catalog
// verbs (databases/tables/describe) are SQL/redis/mongo only; `insert` is
// SQL-family + dataease; dataease is a read-only bypass that offers just doctor +
// insert (everything else is a raw SELECT, no catalog).
func dbcliVerbs(driver string) []string {
	switch driver {
	case "dataease":
		return []string{"doctor", "insert"}
	case "mysql", "postgres", "kingbase", "sqlite":
		return []string{"databases", "tables", "describe", "doctor", "insert"}
	default: // redis, mongo
		return []string{"databases", "tables", "describe", "doctor"}
	}
}

// dbcliBaseFlags are the global flags accepted in any position.
var dbcliBaseFlags = []string{"--target", "--database", "--allow-write", "--pretty", "--verbose", "--timeout", "--config-dir", "--output"}

// dbcliFlags returns the `--<TAB>` flag names for the current (driver, verb)
// context: the `insert` verb adds `--table`, and dataease's insert also adds
// `--exclude` (column filtering, dataease-only).
func dbcliFlags(driver, verb string) []string {
	flags := dbcliBaseFlags
	if verb == "insert" {
		flags = append(append([]string{}, flags...), "--table")
		if driver == "dataease" {
			flags = append(flags, "--exclude")
		}
	}
	return flags
}

type cliFlags struct {
	driver     string
	target     string
	database   string
	allowWrite bool
	configDir  string
	output     string
	timeout    string
	pretty     bool
	verbose    int
	badFlag    string
	legacyEnv  bool   // legacy --env given (renamed to --target)
	file       string // --file: read the SQL from a file ("-" = stdin)
	rest       []string
}

// readSQLFile loads the --file SQL: a path, or "-" for stdin.
func readSQLFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
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
	reg := dbcli.NewRegistry(builtins())
	root := &cobra.Command{
		Use:                "dbcli <driver> [--target n] [--file q.sql] \"<sql>\"",
		Short:              "Query databases (least-privilege, credential-hiding)",
		SilenceUsage:       true,
		SilenceErrors:      true,
		DisableFlagParsing: true,
		ValidArgsFunction: clihelp.ArgvCompleter{
			Tool:       "dbcli",
			Known:      reg.Names(),
			VerbsFor:   dbcliVerbs,
			ValueFlags: []string{"--target", "--database", "--file", "--config-dir", "--output", "--timeout", "--table", "--exclude"},
			FlagsFor:   dbcliFlags,
		}.Complete,
		RunE: func(_ *cobra.Command, args []string) error {
			for _, a := range args {
				if a == "--help" || a == "-h" {
					fmt.Fprintln(os.Stdout, usage)
					os.Exit(0)
				}
				if a == "--version" {
					fmt.Fprintln(os.Stdout, "dbcli version "+buildinfo.Version)
					os.Exit(0)
				}
			}
			f := parseArgs(args)
			raw := f.output == "raw"
			cmd := audit.CommandLine(os.Args)
			// --config-dir must take effect before ANY audit call: audit.writeLine
			// resolves config.Home(), which reads AIDEV_CLIS_HOME — so set it first
			// or early-error lines (BAD_FLAG etc.) get misfiled to the default dir.
			if f.configDir != "" {
				_ = os.Setenv("AIDEV_CLIS_HOME", f.configDir)
			}
			if f.legacyEnv {
				e := errs.New("LEGACY_FLAG", "--env was renamed to --target; pass --target <name> (see `dbcli targets`)", 2)
				audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
				writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
				os.Exit(2)
			}
			if f.badFlag != "" {
				e := errs.New("BAD_FLAG", f.badFlag+" requires a value", 2)
				audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
				writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
				os.Exit(2)
			}
			if f.output != "" && f.output != "json" && f.output != "raw" {
				e := errs.New("UNSUPPORTED_OUTPUT", "dbcli supports --output json|raw", 2)
				audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
				writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
				os.Exit(2)
			}
			if f.driver == "targets" {
				infos, err := config.ListTargets("dbcli")
				op := audit.Begin(audit.Record{Tool: "dbcli", Command: cmd})
				if err != nil {
					op.Finish(err, nil)
					e := errs.From(err)
					writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
					os.Exit(e.Exit)
				}
				op.Finish(nil, nil)
				tctx := context.Background()
				if f.verbose > 0 {
					tctx = diag.With(tctx, diag.New(f.verbose))
				}
				_ = emitTargets(tctx, os.Stdout, infos, raw, f.pretty)
				os.Exit(0)
			}
			if f.driver == "" {
				e := errs.New("NO_DRIVER", "missing <driver>; "+strings.SplitN(usage, "\n", 2)[0], 2)
				audit.Begin(audit.Record{Tool: "dbcli", Command: cmd}).Finish(e, nil)
				writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
				os.Exit(2)
			}
			// --file reads the SQL from a file ("-" = stdin) instead of a 30KB
			// shell-quoted argv token; it becomes the single statement arg.
			if f.file != "" {
				if len(f.rest) > 0 {
					e := errs.New("SQL_ARG_CONFLICT", "pass SQL inline or via --file, not both", 2)
					audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
					writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
					os.Exit(2)
				}
				b, err := readSQLFile(f.file)
				if err != nil {
					e := errs.New("SQL_FILE_UNREADABLE", err.Error(), 2)
					audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
					writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
					os.Exit(2)
				}
				f.rest = []string{string(b)}
			}

			ctx, stop := clihelp.SignalContext()
			defer stop()
			if f.verbose > 0 {
				ctx = diag.With(ctx, diag.New(f.verbose))
			}
			if f.timeout != "" {
				d, err := time.ParseDuration(f.timeout)
				if err != nil {
					e := errs.New("BAD_TIMEOUT", fmt.Sprintf("invalid --timeout %q: %v", f.timeout, err), 2)
					audit.Begin(audit.Record{Tool: "dbcli", Backend: f.driver, Command: cmd}).Finish(e, nil)
					writeCmdErr(os.Stdout, raw, f.pretty, e.Code, e.Message)
					os.Exit(2)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, d)
				defer cancel()
			}

			code := dbcli.Run(ctx, reg, dbcli.RunArgs{
				Driver: f.driver, Target: f.target, Database: f.database, AllowWrite: f.allowWrite,
				Args: f.rest, Out: os.Stdout, Pretty: f.pretty, Raw: raw,
			})
			os.Exit(code)
			return nil
		},
	}
	_ = root.Execute()
}

func parseArgs(args []string) cliFlags {
	var f cliFlags
	takeVal := func(i *int, name string) (string, bool) {
		a := args[*i]
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
		// A value-flag consumes the next token as its value even when that token
		// starts with '-' (e.g. `--database -x`, a negative number or a
		// dash-prefixed identifier). Only value-flags reach takeVal; boolean/count
		// flags are handled in their own switch cases and never consume a token.
		if a == name && *i+1 < len(args) {
			*i++
			return args[*i], true
		}
		return "", false
	}
	take := func(i *int, name string) string {
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
		case a == "--allow-write":
			f.allowWrite = true
		case strings.HasPrefix(a, "--allow-write="):
			f.allowWrite = flagBool(strings.TrimPrefix(a, "--allow-write="))
		case matches(a, "--target"):
			f.target = take(&i, "--target")
		// Legacy selector: --env was renamed to --target (2026-07). Without this
		// case its value leaks into the SQL args and the query resolves against
		// the default target — potentially the wrong database. Reject loudly.
		case matches(a, "--env"):
			f.legacyEnv = true
			take(&i, "--env")
			f.badFlag = ""
		case matches(a, "--database"):
			f.database = take(&i, "--database")
		case matches(a, "--file"):
			f.file = take(&i, "--file")
		case matches(a, "--config-dir"):
			f.configDir = take(&i, "--config-dir")
		case matches(a, "--output"):
			f.output = take(&i, "--output")
		case matches(a, "--timeout"):
			f.timeout = take(&i, "--timeout")
		case f.driver == "" && len(a) > 0 && a[0] != '-':
			f.driver = a
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f
}

func matches(arg, name string) bool { return arg == name || strings.HasPrefix(arg, name+"=") }

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
	diag.From(ctx).Logf(1, "listed %d dbcli targets", len(infos))
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
