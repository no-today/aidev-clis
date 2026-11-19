// Package pg is the dbcli "pg" driver: PostgreSQL over pgx, via the shared
// pgwire dialect. The dbcli "database" namespace is a pg schema.
package pg

import (
	"context"

	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/pgwire"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "postgres" }

func (adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	return pgwire.Run(ctx, in, out, nil)
}
