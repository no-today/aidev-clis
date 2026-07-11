// Package diag collects opt-in diagnostic lines during a single CLI
// invocation. Lines are gathered into an in-memory buffer carried on the
// context, then drained into the output envelope's "diagnostics" field — only
// when the user asked for them via -v/-vv. Default (level 0) records nothing,
// so the stdout contract is byte-for-byte unchanged.
//
// This is deliberately a tiny leaf package (context + sync only) so deep
// packages like exec/dbconn/sqlcore can log into it without pulling in the
// cobra/envelope layers.
package diag

import (
	"context"
	"fmt"
	"sync"
)

// Diag is a verbosity-gated, concurrency-safe line collector. The zero value
// is unusable; build one with New. A nil *Diag is a valid no-op sink, so call
// sites never need a nil check: diag.From(ctx).Logf(...) is always safe.
type Diag struct {
	level int // the -v count: 1 for -v, 2 for -vv, 0 = silent
	mu    sync.Mutex
	lines []string
}

// maxLines bounds the buffer so a long-lived invocation (e.g. `-f` follow under
// -vv) can't grow it without limit. Past the cap, further lines are dropped and
// a single truncation marker is kept.
const maxLines = 10000

// New returns a collector that keeps lines logged at or below level. level is
// the -v count (0 silent, 1 = -v, 2 = -vv).
func New(level int) *Diag { return &Diag{level: level} }

// Logf records a formatted line when level <= the collector's level. level is
// the minimum verbosity at which this line should appear (1 = -v, 2 = -vv).
// Safe on a nil receiver (records nothing). Callers must not pass secrets;
// redaction is the caller's responsibility, same discipline as audit.
func (d *Diag) Logf(level int, format string, args ...any) {
	if d == nil || level < 1 || level > d.level {
		return
	}
	line := fmt.Sprintf(format, args...)
	d.mu.Lock()
	switch {
	case len(d.lines) < maxLines:
		d.lines = append(d.lines, line)
	case len(d.lines) == maxLines:
		d.lines = append(d.lines, "… diagnostics truncated (10000-line cap reached)")
	}
	d.mu.Unlock()
}

// Lines returns a copy of the recorded lines in order, or nil if none. Safe on
// a nil receiver.
func (d *Diag) Lines() []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lines) == 0 {
		return nil
	}
	out := make([]string, len(d.lines))
	copy(out, d.lines)
	return out
}

type ctxKey struct{}

// With returns a context carrying d, retrievable with From.
func With(ctx context.Context, d *Diag) context.Context {
	return context.WithValue(ctx, ctxKey{}, d)
}

// From returns the Diag carried by ctx, or nil if none. The nil is a usable
// no-op sink (see Diag).
func From(ctx context.Context) *Diag {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(ctxKey{}).(*Diag)
	return d
}
