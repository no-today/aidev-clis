// Package sshfile is the logcli "ssh-file" adapter: tails CONFIGURED log files
// on a remote host over SSH. The files are an allow-list from config — the user
// cannot point it at an arbitrary path. Verbs: logs, ls.
package sshfile

import (
	"context"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type adapter struct{ runner exec.Runner } // nil → SSH from config

func New() logcli.Adapter { return adapter{} }

func (adapter) Name() string { return "ssh-file" }

func (a adapter) Run(ctx context.Context, in logcli.Input, out logcli.Output) error {
	files := toStrings(in.Target.Raw["files"])
	verb := "logs"
	if len(in.Args) > 0 && in.Args[0] != "" && in.Args[0][0] != '-' {
		verb = in.Args[0]
	}
	switch verb {
	case "doctor":
		runner := a.runner
		if runner == nil {
			r, err := exec.SSHFromConfig(in.Target.Raw)
			if err != nil {
				return err
			}
			runner = r
		}
		host, _ := in.Target.Raw["host"].(string)
		return logcli.Doctor(out, "ssh "+host, "ssh reachable", func() error {
			return runner.Run(ctx, exec.Spec{Argv: []string{"true"}}, func(string) error { return nil })
		})
	case "ls":
		recs := make([]any, 0, len(files))
		for _, f := range files {
			recs = append(recs, map[string]any{"name": f})
		}
		return out.Batch(recs)
	case "logs":
		if len(files) == 0 {
			return errs.Config("SSH_FILE_NO_FILES", "env block missing 'files'")
		}
		runner := a.runner
		if runner == nil {
			r, err := exec.SSHFromConfig(in.Target.Raw)
			if err != nil {
				return err
			}
			runner = r
		}
		argv := []string{"tail", "-n", tailLines(in.Args)}
		if hasFollow(in.Args) {
			argv = append(argv, "-f")
		}
		argv = append(argv, files...)
		return logcli.StreamLines(out, in.Args, func(emit func(string) error) error {
			return runner.Run(ctx, exec.Spec{Argv: argv}, emit)
		})
	}
	return errs.Config("SSH_FILE_BAD_VERB", "ssh-file supports: logs, ls, doctor")
}

func toStrings(v any) []string {
	xs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func hasFollow(args []string) bool {
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			return true
		}
	}
	return false
}

func tailLines(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--tail" || args[i] == "-n" {
			return args[i+1]
		}
	}
	return "200"
}
