// Package logcli is the logcli skeleton: the Adapter interface, a Registry, and
// the run loop. It names NO concrete adapter (isolation contract rule 1).
package logcli

import (
	"context"

	"github.com/no-today/aidev-clis/internal/core/config"
)

// Input is what an adapter receives: the resolved target + the args after
// `logcli <adapter>` (verb + flags). The adapter parses its own verb/flags.
type Input struct {
	Target config.Target
	Args   []string
}

// Adapter is the logcli adapter interface. An adapter parses its verb and calls
// EXACTLY ONE of out.Batch (finite result) or out.Stream (open-ended).
type Adapter interface {
	Name() string
	Run(ctx context.Context, in Input, out Output) error
}

// Output is provided by the run loop.
type Output interface {
	// Batch emits a finite result set as {"data":[...]} (+ optional warnings).
	Batch(records []any, warnings ...string) error
	// Stream returns a live NDJSON sink for open-ended output (tail / -f).
	Stream() Streamer
}

// Streamer receives records for NDJSON streaming.
type Streamer interface {
	Record(rec any) error
}

// Registry maps an adapter name to its implementation.
type Registry struct{ byName map[string]Adapter }

func NewRegistry(adapters []Adapter) *Registry {
	r := &Registry{byName: map[string]Adapter{}}
	for _, a := range adapters {
		r.byName[a.Name()] = a
	}
	return r
}

func (r *Registry) Get(name string) (Adapter, bool) { a, ok := r.byName[name]; return a, ok }

// Canonical returns the adapter name if it is registered, or "" if unknown.
// Used to compare a CLI subcommand against an env's `adapter`.
func (r *Registry) Canonical(name string) string {
	if a, ok := r.byName[name]; ok {
		return a.Name()
	}
	return ""
}

// Names returns the registered adapter names (unordered).
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}
