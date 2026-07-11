package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/buildinfo"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

var (
	pretty    bool
	configDir string
	verbose   int
	dg        *diag.Diag // the invocation's diagnostic collector (set in PersistentPreRun)
)

// dctx returns a context carrying the diagnostic collector, so the ctx-aware
// envelope writers can drain it (apicli commands don't thread ctx for output).
func dctx() context.Context { return diag.With(context.Background(), dg) }

// businessExit lets `call` emit its {data} envelope (with ok:false) and still
// exit non-zero on a business failure, WITHOUT writing a second {error}.
var businessExit int

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "apicli {call|login|whoami|logout} <app> ... | apps",
		Short:         "Authenticated HTTP for AI agents (credential-hiding, audited)",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			dg = diag.New(verbose) // verbose=0 → records nothing → diagnostics omitted
			if configDir != "" {
				return os.Setenv("AIDEV_CLIS_HOME", configDir)
			}
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&pretty, "pretty", false, "pretty-print JSON")
	root.PersistentFlags().StringVar(&configDir, "config-dir", "", "override ~/.aidev-clis (sets AIDEV_CLIS_HOME)")
	root.PersistentFlags().CountVarP(&verbose, "verbose", "v", `add a "diagnostics" array to JSON output (-v steps/status, -vv more); raw output (--output raw, --curl) has none`)
	root.AddCommand(callCmd(), loginCmd(), whoamiCmd(), logoutCmd(), appsCmd())
	return root
}

func main() {
	if err := newRoot().Execute(); err != nil {
		e := errs.From(err)
		_ = envelope.WriteErrorCtx(dctx(), os.Stdout, e.Code, e.Message, pretty)
		os.Exit(e.Exit)
	}
	os.Exit(businessExit)
}

func emit(payload any) error { return envelope.WriteDataCtx(dctx(), os.Stdout, payload, nil, pretty) }

func emitWarn(payload any, warnings []string) error {
	return envelope.WriteDataCtx(dctx(), os.Stdout, payload, warnings, pretty)
}

// beginAudit opens an audit op for an apicli command. sideEffecting is true only
// for non-GET/HEAD HTTP calls; request carries resolved backend params (nil for
// login/whoami/logout).
func beginAudit(app, env string, sideEffecting bool, request map[string]any) *audit.Op {
	if env == "" {
		env = "_default"
	}
	return audit.Begin(audit.Record{
		Tool: "apicli", Backend: app, Target: env,
		Command: audit.CommandLine(os.Args), Request: request,
		SideEffecting: sideEffecting,
	})
}
