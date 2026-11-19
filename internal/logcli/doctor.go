package logcli

import "github.com/no-today/aidev-clis/internal/core/errs"

// Check is one doctor stage. The doctor envelope `data` is a flat list of these,
// matching jcli/dbcli; overall health is the exit code, not a top-level field.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Doctor emits an adapter's staged health checks: `config` is always ok (the env
// + credential already resolved before dispatch — its detail echoes the resolved
// target for a human), then `connect` runs probe(). The checks envelope is
// emitted even on failure (the failed stage carries the error code); the returned
// error only carries the non-zero exit — the run loop's finalize won't
// double-write over the batch.
func Doctor(out Output, configDetail, okDetail string, probe func() error) error {
	checks := []any{Check{Name: "config", OK: true, Detail: configDetail}}
	if err := probe(); err != nil {
		e := errs.From(err)
		checks = append(checks, Check{Name: "connect", OK: false, Detail: e.Code + ": " + e.Message})
		_ = out.Batch(checks)
		return err
	}
	return out.Batch(append(checks, Check{Name: "connect", OK: true, Detail: okDetail}))
}
