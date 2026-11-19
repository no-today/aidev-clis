// Package exec runs a native subprocess and streams its stdout line-by-line,
// cross-platform. Adapters use it for pass-through backends (kubectl/docker/ssh).
package exec

import "context"

// Spec is a command to run: argv plus injected environment (KEY=VALUE).
type Spec struct {
	Argv []string // argv[0] is the binary
	Env  []string // appended to the process env (e.g. "KUBECONFIG=/path")
}

// Runner runs a Spec and delivers each stdout line to onLine. Implementations:
// Local (this host) and SSH (a remote host).
type Runner interface {
	Run(ctx context.Context, s Spec, onLine func(line string) error) error
}
