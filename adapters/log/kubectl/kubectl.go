// Package kubectl is the logcli "kubectl" adapter: `logs` (pass-through
// `kubectl logs`) + `ls` (curated `kubectl get deployments` → apps). The
// injected kubeconfig should be a logs-only ServiceAccount (see
// docs/SECURITY-MODEL.md); the allowlist is defense-in-depth.
package kubectl

import (
	"context"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/allow"
	"github.com/no-today/aidev-clis/internal/core/cred"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
	"github.com/no-today/aidev-clis/internal/logcli"
)

type adapter struct{ runner exec.Runner }

func New() logcli.Adapter { return adapter{} }

func (adapter) Name() string { return "kubectl" }

// policy: deny only auth/context/target overrides. NOTE: `-n`/`--namespace` are
// intentionally NOT denied — a user may legitimately pass `-n`, and the injected
// `--namespace` is appended LAST so kubectl's last-flag-wins keeps the pin.
var policy = allow.Policy{
	Verbs:     []string{"logs", "ls", "doctor"},
	DenyFlags: []string{"--kubeconfig", "--server", "--token", "--context", "--as", "--as-group", "--as-uid", "--user", "--username", "--password", "--cluster", "--client-certificate", "--client-key", "--insecure-skip-tls-verify"},
}

// defaultTail bounds a pass-through `logs` fetch. Without an explicit --tail,
// `kubectl logs` batch-collects the ENTIRE history into memory; inject a
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
	kcName, _ := in.Target.Raw["kubeconfig_credential"].(string)
	if kcName == "" {
		return errs.Config("KUBECTL_NO_CRED", "target block missing 'kubeconfig_credential'")
	}
	kcPath, err := cred.Path(kcName)
	if err != nil {
		return err
	}
	cmdEnv := []string{"KUBECONFIG=" + kcPath}
	ns, _ := in.Target.Raw["namespace"].(string)
	runner := a.runner
	if runner == nil {
		runner = exec.Local{}
	}

	switch in.Args[0] {
	case "doctor":
		detail := "kubeconfig " + kcName
		if ns != "" {
			detail += ", namespace " + ns
		}
		return logcli.Doctor(out, detail, "API server reachable + authenticated", func() error {
			return runner.Run(ctx, exec.Spec{Argv: []string{"kubectl", "version", "--request-timeout=10s"}, Env: cmdEnv}, func(string) error { return nil })
		})
	case "ls":
		argv := []string{"kubectl", "get", "deployments", "-o",
			`jsonpath={range .items[*]}{.metadata.name}{"\t"}{.spec.selector.matchLabels.app}{"\n"}{end}`}
		if ns != "" {
			argv = append(argv, "--namespace", ns)
		}
		var recs []any
		err := runner.Run(ctx, exec.Spec{Argv: argv, Env: cmdEnv}, func(line string) error {
			parts := strings.SplitN(line, "\t", 2)
			rec := map[string]any{"name": parts[0]}
			if len(parts) == 2 && parts[1] != "" {
				rec["app"] = parts[1]
			}
			recs = append(recs, rec)
			return nil
		})
		if err != nil {
			return err
		}
		return out.Batch(recs)
	case "logs":
		argv := append([]string{"kubectl", "logs"}, withTailDefault(in.Args[1:])...)
		if ns != "" {
			argv = append(argv, "--namespace", ns)
		}
		return logcli.StreamLines(out, in.Args, func(emit func(string) error) error {
			return runner.Run(ctx, exec.Spec{Argv: argv, Env: cmdEnv}, emit)
		})
	}
	return errs.Config("KUBECTL_BAD_VERB", "kubectl supports: logs, ls, doctor")
}
