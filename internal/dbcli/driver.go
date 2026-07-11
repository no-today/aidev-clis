// Package dbcli is the dbcli skeleton: the Driver interface, a Registry, and the
// run loop. It names NO concrete driver (isolation contract rule 1).
package dbcli

import (
	"context"

	"github.com/no-today/aidev-clis/internal/core/config"
)

// Input is what a driver receives: the resolved target + the args after
// `dbcli <driver>` (raw SQL or a verb), plus the peeled dbcli flags.
type Input struct {
	Target     config.Target
	Args       []string // raw SQL, or a verb (databases/tables/describe/doctor) + its args
	Database   string   // --database (default namespace / scope)
	AllowWrite bool     // --allow-write
}

// Result is a read result set: ordered column names + rows of coerced values.
type Result struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// Driver is the dbcli driver interface. A driver parses its verb/SQL and calls
// out.Batch exactly once with the payload (Result for reads, a small object for
// writes/describe).
type Driver interface {
	Name() string
	Run(ctx context.Context, in Input, out Output) error
}

// Output is provided by the run loop. dbcli is Batch-only (no streaming).
type Output interface {
	// Batch emits {"data":payload} (+ optional warnings). Call exactly once.
	Batch(payload any, warnings ...string) error
}

// RawOutput is an optional Output capability: a verb that emits pre-formatted
// text (e.g. `insert` → raw INSERT statements) instead of the JSON envelope.
// Callers type-assert Output to RawOutput to use it; the run loop's output
// implements it.
type RawOutput interface {
	// Raw writes s verbatim and marks the output as written. It may be called
	// repeatedly to stream output (e.g. one INSERT per row).
	Raw(s string) error
}

// Registry maps a driver name to its implementation.
type Registry struct{ byName map[string]Driver }

func NewRegistry(drivers []Driver) *Registry {
	r := &Registry{byName: map[string]Driver{}}
	for _, d := range drivers {
		r.byName[d.Name()] = d
	}
	return r
}

func (r *Registry) Get(name string) (Driver, bool) { d, ok := r.byName[name]; return d, ok }

// Names returns the registered driver names (unordered).
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}
