// Package kingbase is the dbcli "kingbase" driver: KingbaseES speaks the pg wire
// protocol, so it reuses the shared pgwire dialect, adding KingbaseES's
// Oracle-compat system schemas to the discovery exclusion list.
package kingbase

import (
	"context"

	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/pgwire"
)

// kingbase's extra system schemas (Oracle-compat mode) — excluded from discovery
// so they don't drown the user's tables (the old repo's multi-schema pain).
var systemSchemas = []string{"sys", "sysaudit", "oracle", "sys_catalog"}

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "kingbase" }

func (adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	return pgwire.Run(ctx, in, out, systemSchemas)
}
