// Package pgwire is the shared dbcli dialect for PostgreSQL-protocol engines
// (pg, kingbase) over pgx. The dbcli "database" namespace maps to a pg SCHEMA;
// the outer pg database is fixed per env in the DSN.
package pgwire

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" sql driver

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/dbconn"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

// Run opens the connection (URL DSN used directly; sslmode defaults to disable)
// and dispatches via sqlcore. extraSystemSchemas lets kingbase exclude its
// Oracle-compat catalogs from discovery.
func Run(ctx context.Context, in dbcli.Input, out dbcli.Output, extraSystemSchemas []string) error {
	u, cleanup, err := dbconn.Resolve(ctx, in.Target)
	if err != nil {
		return err
	}
	defer cleanup()

	q := u.Query()
	if q.Get("sslmode") == "" {
		if sm, _ := in.Target.Raw["sslmode"].(string); sm != "" {
			q.Set("sslmode", sm)
		} else {
			q.Set("sslmode", "disable") // project policy: no TLS unless asked
		}
	}
	u.RawQuery = q.Encode()

	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return errs.Config("PG_OPEN", dbconn.Redact(err.Error(), u))
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(30 * time.Second)
	// Connection failures surface lazily inside sqlcore (DB_PING/DB_QUERY) where
	// the URL isn't available; redact the injected password at this boundary.
	return dbconn.RedactErr(sqlcore.Run(ctx, Dialect{ExtraSystemSchemas: extraSystemSchemas}, db, in, out), u)
}
