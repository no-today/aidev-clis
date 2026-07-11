package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/buildinfo"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/tcli"
)

var (
	pretty       bool
	configDir    string
	verbose      int
	tags         []string
	dg           *diag.Diag
	businessExit int // 让 run 出 {data} 信封后按 verdict 退出码退出
)

func dctx() context.Context { return diag.With(context.Background(), dg) }

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tcli {run|validate|explain} <case-or-dir> ...",
		Short:         "Post-deploy one-shot verification gate (orchestrates apicli/dbcli/logcli)",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			dg = diag.New(verbose)
			if configDir != "" {
				return os.Setenv("AIDEV_CLIS_HOME", configDir)
			}
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&pretty, "pretty", false, "pretty-print JSON")
	root.PersistentFlags().StringVar(&configDir, "config-dir", "", "override ~/.aidev-clis (sets AIDEV_CLIS_HOME)")
	root.PersistentFlags().CountVarP(&verbose, "verbose", "v", `add a "diagnostics" array to JSON output`)
	root.PersistentFlags().StringArrayVar(&tags, "tag", nil, "only cases with all these tags (repeatable, AND)")
	root.AddCommand(runCmd(), validateCmd(), explainCmd())
	wireCompletions(root)
	return root
}

func emit(payload any) error { return envelope.WriteDataCtx(dctx(), os.Stdout, payload, nil, pretty) }

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use: "run <case-or-dir>", Args: cobra.ExactArgs(1),
		Short: "Run a verification case (or every case in a directory) and emit a verdict + CI exit code",
		RunE: func(_ *cobra.Command, args []string) error {
			r := tcli.NewRunner(configDir)
			payload, exit, err := tcli.RunCases(dctx(), r, args[0], tags)
			if err != nil {
				return err
			}
			businessExit = exit
			return emit(payload)
		},
	}
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use: "validate <case-or-dir>", Args: cobra.ExactArgs(1),
		Short: "Statically check case files (schema, references) without running anything",
		RunE: func(_ *cobra.Command, args []string) error {
			payload, exit, err := tcli.ValidateCases(args[0], tags)
			if err != nil {
				return err
			}
			businessExit = exit
			return emit(payload)
		},
	}
}

func explainCmd() *cobra.Command {
	return &cobra.Command{
		Use: "explain <case-or-dir>", Args: cobra.ExactArgs(1),
		Short: "Describe what a case will do (steps, targets, assertions) without running it",
		RunE: func(_ *cobra.Command, args []string) error {
			r := tcli.NewRunner(configDir)
			payload, _, err := tcli.ExplainCases(dctx(), r, args[0], tags)
			if err != nil {
				return err
			}
			return emit(payload)
		},
	}
}

func main() {
	if err := newRoot().Execute(); err != nil {
		e := errs.From(err)
		_ = envelope.WriteErrorCtx(dctx(), os.Stdout, e.Code, e.Message, pretty)
		os.Exit(e.Exit)
	}
	os.Exit(businessExit)
}
