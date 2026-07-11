// Package clihelp provides the small, OPT-IN helpers shared by the CLIs.
package clihelp

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalContext returns a context cancelled on SIGINT/SIGTERM.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
