// Package mysql is the dbcli "mysql" driver: connects via go-sql-driver and
// delegates execution to sqlcore. The injected read-only account is the primary
// boundary; the sqlcore statement guard is defense-in-depth.
package mysql

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
	"github.com/no-today/aidev-clis/internal/dbcli/dbconn"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

type adapter struct{}

func New() dbcli.Driver { return adapter{} }

func (adapter) Name() string { return "mysql" }

func (a adapter) Run(ctx context.Context, in dbcli.Input, out dbcli.Output) error {
	u, cleanup, err := dbconn.Resolve(ctx, in.Target)
	if err != nil {
		return err
	}
	defer cleanup()
	db, err := sql.Open("mysql", toMySQLDSN(u))
	if err != nil {
		return errs.Config("MYSQL_OPEN", dbconn.Redact(err.Error(), u))
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(30 * time.Second)
	// Connection failures surface lazily inside sqlcore (DB_PING/DB_QUERY) where
	// the URL isn't available; redact the injected password at this boundary.
	return dbconn.RedactErr(sqlcore.Run(ctx, dialect{}, db, in, out), u)
}

// toMySQLDSN converts the resolved URL DSN into go-sql-driver's DSN form.
func toMySQLDSN(u *url.URL) string {
	cfg := mysqldrv.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.DBName = strings.TrimPrefix(u.Path, "/")
	cfg.ParseTime = true
	cfg.Params = map[string]string{}
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			cfg.Params[k] = vs[0]
		}
	}
	return cfg.FormatDSN()
}
