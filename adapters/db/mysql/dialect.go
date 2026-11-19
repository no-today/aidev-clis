package mysql

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

type dialect struct{}

func (dialect) DriverName() string { return "mysql" }

func (dialect) SystemNamespaces() []string {
	return []string{"information_schema", "mysql", "performance_schema", "sys"}
}

func (d dialect) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT schema_name FROM information_schema.schemata "+
			"WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys') "+
			"ORDER BY schema_name")
	if err != nil {
		return nil, errs.Remote("MYSQL_CATALOG", err.Error())
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, errs.Remote("MYSQL_SCAN", err.Error())
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d dialect) ListTables(ctx context.Context, db *sql.DB, database, like string) ([]sqlcore.Table, error) {
	q := "SELECT table_schema, table_name FROM information_schema.tables " +
		"WHERE table_type='BASE TABLE' AND table_schema NOT IN ('information_schema','mysql','performance_schema','sys')"
	var args []any
	if database != "" {
		q += " AND table_schema = ?"
		args = append(args, database)
	}
	if like != "" {
		q += " AND table_name LIKE ?"
		args = append(args, like)
	}
	q += " ORDER BY table_schema, table_name"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errs.Remote("MYSQL_CATALOG", err.Error())
	}
	defer rows.Close()
	var out []sqlcore.Table
	for rows.Next() {
		var t sqlcore.Table
		if err := rows.Scan(&t.Database, &t.Name); err != nil {
			return nil, errs.Remote("MYSQL_SCAN", err.Error())
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (dialect) UseDatabase(ctx context.Context, conn *sql.Conn, database string) error {
	// database is an identifier; backtick-quote and double any embedded backtick.
	_, err := conn.ExecContext(ctx, "USE `"+quoteIdent(database)+"`")
	if err != nil {
		return errs.Config("MYSQL_USE_DB", err.Error())
	}
	return nil
}

func (dialect) BackendID(ctx context.Context, conn *sql.Conn) (string, error) {
	var id int64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

func (dialect) CancelQuery(ctx context.Context, db *sql.DB, backendID string) error {
	if _, err := strconv.ParseInt(backendID, 10, 64); err != nil {
		return nil // not a numeric id; nothing safe to kill
	}
	_, err := db.ExecContext(ctx, "KILL QUERY "+backendID)
	return err
}

func quoteIdent(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '`' {
			out = append(out, '`')
		}
		out = append(out, r)
	}
	return string(out)
}
