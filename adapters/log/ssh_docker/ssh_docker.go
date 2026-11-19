// Package sshdocker is the logcli "ssh-docker" adapter: `docker logs` / `docker
// ps` on a remote host over SSH. Same pass-through allowlist as the local docker
// adapter; the SSH transport is the only difference. Verbs: logs, ls.
package sshdocker

import (
	"context"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/allow"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type adapter struct{ runner exec.Runner } // nil → SSH from config

func New() logcli.Adapter { return adapter{} }

func (adapter) Name() string { return "ssh-docker" }

var policy = allow.Policy{
	Verbs:     []string{"logs", "ls", "doctor"},
	DenyFlags: []string{"--host", "-H", "--config", "--context", "--tlscacert"},
}

// defaultTail bounds a pass-through `logs` fetch. Without an explicit --tail,
// `docker logs` batch-collects the ENTIRE history into memory; inject a
// bounded default (matches local_file's 200) unless the user set their own
// --tail. Applied in follow mode too, so the initial backlog stays bounded.
const defaultTail = "200"

func withTailDefault(args []string) []string {
	for _, a := range args {
		if a == "--tail" || strings.HasPrefix(a, "--tail=") {
			return args
		}
	}
	return append(args[:len(args):len(args)], "--tail="+defaultTail)
}

func (a adapter) Run(ctx context.Context, in logcli.Input, out logcli.Output) error {
	if err := policy.Check(in.Args); err != nil {
		return err
	}
	runner := a.runner
	if runner == nil {
		r, err := exec.SSHFromConfig(in.Target.Raw)
		if err != nil {
			return err
		}
		runner = r
	}
	switch in.Args[0] {
	case "doctor":
		host, _ := in.Target.Raw["host"].(string)
		return logcli.Doctor(out, "ssh "+host, "ssh + docker daemon ok", func() error {
			return runner.Run(ctx, exec.Spec{Argv: []string{"docker", "version"}}, func(string) error { return nil })
		})
	case "ls":
		argv := []string{"docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}"}
		var recs []any
		err := runner.Run(ctx, exec.Spec{Argv: argv}, func(line string) error {
			p := strings.SplitN(line, "\t", 3)
			rec := map[string]any{"name": p[0]}
			if len(p) > 1 {
				rec["image"] = p[1]
			}
			if len(p) > 2 {
				rec["status"] = p[2]
			}
			recs = append(recs, rec)
			return nil
		})
		if err != nil {
			return err
		}
		return out.Batch(recs)
	case "logs":
		argv := append([]string{"docker", "logs"}, withTailDefault(in.Args[1:])...)
		return logcli.StreamLines(out, in.Args, func(emit func(string) error) error {
			return runner.Run(ctx, exec.Spec{Argv: argv}, emit)
		})
	}
	return errs.Config("SSH_DOCKER_BAD_VERB", "ssh-docker supports: logs, ls, doctor")
}
