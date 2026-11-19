package pgwire

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

// Dialect implements sqlcore.Dialect for the pg wire protocol. ExtraSystemSchemas
// are excluded from discovery in addition to the always-excluded pg system ones.
type Dialect struct {
	ExtraSystemSchemas []string
}

func (Dialect) DriverName() string { return "pgx" }

func (d Dialect) SystemNamespaces() []string {
	return append([]string{"pg_catalog", "information_schema", "pg_toast"}, d.ExtraSystemSchemas...)
}

// excludeList renders SystemNamespaces() as a comma-separated quoted SQL list.
func (d Dialect) excludeList() string {
	ns := d.SystemNamespaces()
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = "'" + strings.ReplaceAll(n, "'", "''") + "'"
	}
	return strings.Join(parts, ",")
}

func (d Dialect) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT schema_name FROM information_schema.schemata "+
			"WHERE schema_name NOT IN ("+d.excludeList()+") AND schema_name NOT LIKE 'pg\\_%' "+
			"ORDER BY schema_name")
	if err != nil {
		return nil, errs.Remote("PG_CATALOG", err.Error())
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, errs.Remote("PG_SCAN", err.Error())
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d Dialect) ListTables(ctx context.Context, db *sql.DB, database, like string) ([]sqlcore.Table, error) {
	q := "SELECT schemaname, tablename FROM pg_tables WHERE schemaname NOT IN (" + d.excludeList() + ") AND schemaname NOT LIKE 'pg\\_%'"
	var args []any
	n := 1
	if database != "" {
		q += " AND schemaname = $" + strconv.Itoa(n)
		args = append(args, database)
		n++
	}
	if like != "" {
		q += " AND tablename LIKE $" + strconv.Itoa(n)
		args = append(args, like)
	}
	q += " ORDER BY schemaname, tablename"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errs.Remote("PG_CATALOG", err.Error())
	}
	defer rows.Close()
	var out []sqlcore.Table
	for rows.Next() {
		var t sqlcore.Table
		if err := rows.Scan(&t.Database, &t.Name); err != nil {
			return nil, errs.Remote("PG_SCAN", err.Error())
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (Dialect) UseDatabase(ctx context.Context, conn *sql.Conn, database string) error {
	// quote the schema identifier; double any embedded double-quote.
	_, err := conn.ExecContext(ctx, `SET search_path TO "`+strings.ReplaceAll(database, `"`, `""`)+`"`)
	if err != nil {
		return errs.Config("PG_SET_SEARCH_PATH", err.Error())
	}
	return nil
}

func (Dialect) BackendID(ctx context.Context, conn *sql.Conn) (string, error) {
	var pid int64
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		return "", err
	}
	return strconv.FormatInt(pid, 10), nil
}

func (Dialect) CancelQuery(ctx context.Context, db *sql.DB, backendID string) error {
	pid, err := strconv.ParseInt(backendID, 10, 64)
	if err != nil {
		return nil
	}
	// polite cancel of the running query; terminate as a fallback.
	if _, err := db.ExecContext(ctx, "SELECT pg_cancel_backend($1)", pid); err != nil {
		_, _ = db.ExecContext(ctx, "SELECT pg_terminate_backend($1)", pid)
	}
	return nil
}
