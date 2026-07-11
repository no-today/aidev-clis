// Package sqlite is the dbcli "sqlite" driver: a local SQLite file via the
// pure-Go modernc.org/sqlite driver, delegating to sqlcore. There is no
// credential or tunnel — open the file `?mode=ro` for a read-only boundary.
package sqlite

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" sql driver

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "sqlite" }

func (adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	dsn, _ := in.Target.Raw["dsn"].(string)
	if dsn == "" {
		return errs.Config("DSN_MISSING", "sqlite target needs 'dsn' (a file path or file: URI, e.g. file:/var/lib/app.db?mode=ro)")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return errs.Config("SQLITE_OPEN", err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // one fd to the file avoids "database is locked"
	db.SetConnMaxLifetime(30 * time.Second)
	return sqlcore.Run(ctx, dialect{}, db, in, out)
}
