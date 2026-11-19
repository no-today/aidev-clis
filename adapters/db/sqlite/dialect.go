package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

type dialect struct{}

func (dialect) DriverName() string         { return "sqlite" }
func (dialect) SystemNamespaces() []string { return nil }

func (dialect) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, errs.Remote("SQLITE_CATALOG", err.Error())
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return nil, errs.Remote("SQLITE_SCAN", err.Error())
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (dialect) ListTables(ctx context.Context, db *sql.DB, database, like string) ([]sqlcore.Table, error) {
	master := "sqlite_master"
	if database != "" {
		master = quoteIdent(database) + ".sqlite_master"
	}
	q := "SELECT name FROM " + master + " WHERE type IN ('table','view') AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\'"
	var args []any
	if like != "" {
		q += " AND name LIKE ?"
		args = append(args, like)
	}
	q += " ORDER BY name"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, errs.Remote("SQLITE_CATALOG", err.Error())
	}
	defer rows.Close()
	dbname := database
	if dbname == "" {
		dbname = "main"
	}
	var out []sqlcore.Table
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, errs.Remote("SQLITE_SCAN", err.Error())
		}
		out = append(out, sqlcore.Table{Database: dbname, Name: name})
	}
	return out, rows.Err()
}

func (dialect) Describe(ctx context.Context, db *sql.DB, database, table string) (sqlcore.TableSchema, error) {
	dbname := database
	if dbname == "" {
		dbname = "main"
	}
	ts := sqlcore.TableSchema{Database: dbname, Table: table}

	// PRAGMA does not take placeholders → interpolate as a quoted identifier.
	cr, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return ts, errs.Remote("SQLITE_CATALOG", err.Error())
	}
	defer cr.Close()
	for cr.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := cr.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return ts, errs.Remote("SQLITE_SCAN", err.Error())
		}
		c := sqlcore.Column{Name: name, Type: typ, Nullable: notnull == 0, Default: dflt.String}
		if pk > 0 {
			c.Key = "PRI"
		}
		ts.Columns = append(ts.Columns, c)
	}
	if len(ts.Columns) == 0 {
		return ts, errs.Config("TABLE_NOT_FOUND", "no table "+table)
	}

	// indexes: index_list → index_info per index
	il, err := db.QueryContext(ctx, "PRAGMA index_list("+quoteIdent(table)+")")
	if err != nil {
		return ts, errs.Remote("SQLITE_CATALOG", err.Error())
	}
	type idxRow struct {
		name   string
		unique bool
	}
	var idxs []idxRow
	for il.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := il.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			il.Close()
			return ts, errs.Remote("SQLITE_SCAN", err.Error())
		}
		idxs = append(idxs, idxRow{name, unique == 1})
	}
	il.Close()
	for _, ix := range idxs {
		cols, err := indexColumns(ctx, db, ix.name)
		if err != nil {
			return ts, err
		}
		ts.Indexes = append(ts.Indexes, sqlcore.Index{Name: ix.name, Columns: cols, Unique: ix.unique})
	}
	return ts, nil
}

func indexColumns(ctx context.Context, db *sql.DB, index string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA index_info("+quoteIdent(index)+")")
	if err != nil {
		return nil, errs.Remote("SQLITE_CATALOG", err.Error())
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name sql.NullString
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, errs.Remote("SQLITE_SCAN", err.Error())
		}
		cols = append(cols, name.String)
	}
	return cols, rows.Err()
}

// sqlite has no USE statement: a raw statement runs against whatever schema it
// names (main, or an attached db via a qualified name). Silently ignoring
// --database would let an agent believe it scoped the statement when it didn't,
// so surface it as a clear error — but only when --database is actually set, so
// the unqualified path stays a no-op. (The tables/describe verbs never call this;
// they pass --database straight into schema-qualified catalog queries.)
func (dialect) UseDatabase(_ context.Context, _ *sql.Conn, database string) error {
	if database != "" {
		return errs.Config("SQLITE_NO_DATABASE",
			"sqlite has no USE; --database is not supported for raw statements — use a schema-qualified name (e.g. attached_db.table)")
	}
	return nil
}
func (dialect) BackendID(context.Context, *sql.Conn) (string, error) { return "", nil }
func (dialect) CancelQuery(context.Context, *sql.DB, string) error   { return nil }

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
