package mysql

import (
	"context"
	"database/sql"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

func (d dialect) Describe(ctx context.Context, db *sql.DB, database, table string) (sqlcore.TableSchema, error) {
	database, meta, err := d.resolveTable(ctx, db, database, table)
	if err != nil {
		return sqlcore.TableSchema{}, err
	}
	ts := sqlcore.TableSchema{Database: database, Table: table, Comment: meta.comment, Charset: meta.charset, Collation: meta.collation}

	cr, err := db.QueryContext(ctx,
		"SELECT column_name, column_type, is_nullable, column_key, column_default, "+
			"column_comment, character_set_name, collation_name, extra "+
			"FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position",
		database, table)
	if err != nil {
		return ts, errs.Remote("MYSQL_CATALOG", err.Error())
	}
	defer cr.Close()
	for cr.Next() {
		var c sqlcore.Column
		var nullable string
		var colDefault, charset, collation sql.NullString
		var comment, extra string
		if err := cr.Scan(&c.Name, &c.Type, &nullable, &c.Key, &colDefault, &comment, &charset, &collation, &extra); err != nil {
			return ts, errs.Remote("MYSQL_SCAN", err.Error())
		}
		c.Nullable = nullable == "YES"
		if colDefault.Valid {
			c.Default = colDefault.String
		}
		c.Comment = comment
		c.Charset = charset.String
		c.Collation = collation.String
		c.Extra = extra
		ts.Columns = append(ts.Columns, c)
	}

	ir, err := db.QueryContext(ctx,
		"SELECT index_name, column_name, non_unique FROM information_schema.statistics "+
			"WHERE table_schema=? AND table_name=? ORDER BY index_name, seq_in_index",
		database, table)
	if err != nil {
		return ts, errs.Remote("MYSQL_CATALOG", err.Error())
	}
	defer ir.Close()
	idx := map[string]*sqlcore.Index{}
	var order []string
	for ir.Next() {
		var name, col string
		var nonUnique int64
		if err := ir.Scan(&name, &col, &nonUnique); err != nil {
			return ts, errs.Remote("MYSQL_SCAN", err.Error())
		}
		if idx[name] == nil {
			idx[name] = &sqlcore.Index{Name: name, Unique: nonUnique == 0}
			order = append(order, name)
		}
		idx[name].Columns = append(idx[name].Columns, col)
	}
	for _, n := range order {
		ts.Indexes = append(ts.Indexes, *idx[n])
	}
	return ts, nil
}

// tableMeta is the table-level metadata describe surfaces (charset is the
// prefix of the collation, e.g. utf8mb4_general_ci → utf8mb4).
type tableMeta struct {
	comment, charset, collation string
}

// resolveTable returns the database holding table (+ its metadata). database may
// be given; if empty it is discovered, erroring on 0 or >1 matches.
func (d dialect) resolveTable(ctx context.Context, db *sql.DB, database, table string) (string, tableMeta, error) {
	q := "SELECT t.table_schema, COALESCE(t.table_comment,''), COALESCE(t.table_collation,''), COALESCE(ccsa.character_set_name,'') " +
		"FROM information_schema.tables t " +
		"LEFT JOIN information_schema.collation_character_set_applicability ccsa ON ccsa.collation_name = t.table_collation " +
		"WHERE t.table_type='BASE TABLE' AND t.table_name=? AND t.table_schema NOT IN ('information_schema','mysql','performance_schema','sys')"
	args := []any{table}
	if database != "" {
		q += " AND t.table_schema=?"
		args = append(args, database)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return "", tableMeta{}, errs.Remote("MYSQL_CATALOG", err.Error())
	}
	defer rows.Close()
	var schemas []string
	var meta tableMeta
	for rows.Next() {
		var s string
		var m tableMeta
		if err := rows.Scan(&s, &m.comment, &m.collation, &m.charset); err != nil {
			return "", tableMeta{}, errs.Remote("MYSQL_SCAN", err.Error())
		}
		schemas = append(schemas, s)
		meta = m
	}
	switch len(schemas) {
	case 0:
		return "", tableMeta{}, errs.Config("TABLE_NOT_FOUND", "no table "+table)
	case 1:
		return schemas[0], meta, nil
	default:
		return "", tableMeta{}, errs.Config("TABLE_AMBIGUOUS",
			"table "+table+" exists in: "+strings.Join(schemas, ", ")+"; qualify it as <database>."+table)
	}
}
