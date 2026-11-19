// Package docker is the logcli "docker" adapter: `logs` (pass-through
// `docker logs`) + `ls` (`docker ps`). Optional DOCKER_HOST/TLS from config.
package docker

import (
	"context"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/allow"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type adapter struct{ runner exec.Runner }

func New() logcli.Adapter { return adapter{} }

func (adapter) Name() string { return "docker" }

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
	var cmdEnv []string
	if dh, _ := in.Target.Raw["docker_host"].(string); dh != "" {
		cmdEnv = append(cmdEnv, "DOCKER_HOST="+dh)
	}
	if cp, _ := in.Target.Raw["docker_cert_path"].(string); cp != "" {
		cmdEnv = append(cmdEnv, "DOCKER_CERT_PATH="+cp, "DOCKER_TLS_VERIFY=1")
	}
	runner := a.runner
	if runner == nil {
		runner = exec.Local{}
	}

	switch in.Args[0] {
	case "doctor":
		host := "local socket"
		if dh, _ := in.Target.Raw["docker_host"].(string); dh != "" {
			host = dh
		}
		return logcli.Doctor(out, "docker host "+host, "daemon reachable", func() error {
			return runner.Run(ctx, exec.Spec{Argv: []string{"docker", "version"}, Env: cmdEnv}, func(string) error { return nil })
		})
	case "ls":
		argv := []string{"docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}"}
		var recs []any
		err := runner.Run(ctx, exec.Spec{Argv: argv, Env: cmdEnv}, func(line string) error {
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
			return runner.Run(ctx, exec.Spec{Argv: argv, Env: cmdEnv}, emit)
		})
	}
	return errs.Config("DOCKER_BAD_VERB", "docker supports: logs, ls, doctor")
}
