package main

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/no-today/aidev-clis/internal/core/audit"
	"github.com/no-today/aidev-clis/internal/core/buildinfo"
	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/envelope"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/jcli"
)

var (
	pretty    bool
	configDir string
	verbose   int
	dg        *diag.Diag // the invocation's diagnostic collector (set in PersistentPreRun)
)

// dctx returns a context carrying the diagnostic collector, so the ctx-aware
// envelope writers can drain it. jcli commands don't thread ctx for output, so
// the collector rides a package var instead.
func dctx() context.Context { return diag.With(context.Background(), dg) }

// nowRFC3339 is the single timestamp source (e.g. the jobs-cache synced_at).
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "jcli <verb> [--target n] <service>",
		Short:         "Trigger and watch Jenkins builds (credential-hiding, audited)",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			dg = diag.New(verbose) // verbose=0 → records nothing → diagnostics omitted
			if configDir != "" {
				return os.Setenv("AIDEV_CLIS_HOME", configDir)
			}
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&pretty, "pretty", false, "pretty-print JSON")
	root.PersistentFlags().StringVar(&configDir, "config-dir", "", "override ~/.aidev-clis (sets AIDEV_CLIS_HOME)")
	root.PersistentFlags().CountVarP(&verbose, "verbose", "v", `add a "diagnostics" array to JSON output (-v steps/timing, -vv more); raw output (log --output raw) has none`)
	root.AddCommand(targetsCmd(), deployCmd(), buildCmd(), statusCmd(), logCmd(), cancelCmd(), jobsCmd(), paramsCmd(), doctorCmd())
	wireCompletions(root)
	return root
}

// doctorExit lets `doctor` emit its {data} envelope and still exit non-zero when a
// stage fails (the health is the exit code, not a field in the data).
var doctorExit int

func main() {
	if err := newRoot().Execute(); err != nil {
		e := errs.From(err)
		_ = envelope.WriteErrorCtx(dctx(), os.Stdout, e.Code, e.Message, pretty)
		os.Exit(e.Exit)
	}
	os.Exit(doctorExit)
}

func emit(payload any) error { return envelope.WriteDataCtx(dctx(), os.Stdout, payload, nil, pretty) }

func loadTarget(flagTarget string) (config.Target, *jcli.Config, error) {
	target, err := config.Resolve("jcli", flagTarget)
	if err != nil {
		return target, nil, err
	}
	cfg, err := jcli.ParseConfig(target.Raw)
	return target, cfg, err
}

// beginJob opens an audit op for a jcli command. sideEffecting is true only for
// the backend-mutating ops (build/deploy/cancel) — they are also the only ops that
// carry a request layer, so job/params are populated for them and left empty for
// reads. Reads (and pre-dispatch failures) go through auditRead, which passes
// empties here: the service they operated on is already visible in the audited
// command (full argv), so per spec only side-effecting ops record {job, params}.
func beginJob(targetName string, sideEffecting bool, job string, params map[string]string) *audit.Op {
	var req map[string]any
	if job != "" || len(params) > 0 {
		req = map[string]any{}
		if job != "" {
			req["job"] = job
		}
		if len(params) > 0 {
			req["params"] = params
		}
	}
	return audit.Begin(audit.Record{
		Tool: "jcli", Backend: "jenkins", Target: targetName,
		Command: audit.CommandLine(os.Args), Request: req,
		SideEffecting: sideEffecting,
	})
}

// auditRead writes a single terminal audit line for a read or a pre-dispatch
// failure — no request, never side-effecting. (Side-effecting build/deploy/cancel
// use beginJob(..., true, job, params) for their two-phase started+terminal lines.)
func auditRead(targetName string, err error) {
	beginJob(targetName, false, "", nil).Finish(err, nil)
}

func targetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets",
		Short: "List configured Jenkins instances",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			infos, err := config.ListTargets("jcli")
			auditRead("", err)
			if err != nil {
				return err
			}
			return emit(infos)
		},
	}
}
