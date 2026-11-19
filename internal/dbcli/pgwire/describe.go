package pgwire

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli/sqlcore"
)

func (d Dialect) Describe(ctx context.Context, db *sql.DB, database, table string) (sqlcore.TableSchema, error) {
	schema, err := d.resolveTable(ctx, db, database, table)
	if err != nil {
		return sqlcore.TableSchema{}, err
	}
	ts := sqlcore.TableSchema{Database: schema, Table: table}

	cr, err := db.QueryContext(ctx,
		"SELECT column_name, data_type, is_nullable, column_default, "+
			"character_maximum_length, numeric_precision, numeric_scale, collation_name, "+
			"is_identity, is_generated, "+
			"col_description((quote_ident(table_schema)||'.'||quote_ident(table_name))::regclass, ordinal_position) "+
			"FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 ORDER BY ordinal_position",
		schema, table)
	if err != nil {
		return ts, errs.Remote("PG_CATALOG", err.Error())
	}
	defer cr.Close()
	for cr.Next() {
		var c sqlcore.Column
		var nullable, dataType string
		var colDefault, collation, comment, identity, generated sql.NullString
		var charMaxLen, numPrecision, numScale sql.NullInt64
		if err := cr.Scan(&c.Name, &dataType, &nullable, &colDefault,
			&charMaxLen, &numPrecision, &numScale, &collation,
			&identity, &generated, &comment); err != nil {
			return ts, errs.Remote("PG_SCAN", err.Error())
		}
		c.Type = pgType(dataType, charMaxLen, numPrecision, numScale)
		c.Nullable = nullable == "YES"
		c.Default = colDefault.String
		c.Collation = collation.String
		c.Comment = comment.String
		c.Extra = pgExtra(identity, generated)
		ts.Columns = append(ts.Columns, c)
	}

	ir, err := db.QueryContext(ctx,
		"SELECT i.relname AS index_name, a.attname AS column_name, ix.indisunique "+
			"FROM pg_index ix "+
			"JOIN pg_class i ON i.oid = ix.indexrelid "+
			"JOIN pg_class t ON t.oid = ix.indrelid "+
			"JOIN pg_namespace n ON n.oid = t.relnamespace "+
			"JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true "+
			"JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum "+
			"WHERE n.nspname=$1 AND t.relname=$2 ORDER BY i.relname, k.ord",
		schema, table)
	if err != nil {
		return ts, errs.Remote("PG_CATALOG", err.Error())
	}
	defer ir.Close()
	idx := map[string]*sqlcore.Index{}
	var order []string
	for ir.Next() {
		var name, col string
		var unique bool
		if err := ir.Scan(&name, &col, &unique); err != nil {
			return ts, errs.Remote("PG_SCAN", err.Error())
		}
		if idx[name] == nil {
			idx[name] = &sqlcore.Index{Name: name, Unique: unique}
			order = append(order, name)
		}
		idx[name].Columns = append(idx[name].Columns, col)
	}
	for _, n := range order {
		ts.Indexes = append(ts.Indexes, *idx[n])
	}

	var comment sql.NullString
	_ = db.QueryRowContext(ctx,
		"SELECT obj_description(($1||'.'||$2)::regclass, 'pg_class')", schema, table).Scan(&comment)
	ts.Comment = comment.String
	return ts, nil
}

// resolveTable returns the schema holding table; discovers it when database is
// empty, erroring on 0 (TABLE_NOT_FOUND) or >1 (TABLE_AMBIGUOUS) matches.
func (d Dialect) resolveTable(ctx context.Context, db *sql.DB, database, table string) (string, error) {
	if database != "" {
		return database, nil
	}
	rows, err := db.QueryContext(ctx,
		"SELECT schemaname FROM pg_tables WHERE tablename=$1 AND schemaname NOT IN ("+d.excludeList()+") AND schemaname NOT LIKE 'pg\\_%' ORDER BY schemaname",
		table)
	if err != nil {
		return "", errs.Remote("PG_CATALOG", err.Error())
	}
	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", errs.Remote("PG_SCAN", err.Error())
		}
		schemas = append(schemas, s)
	}
	switch len(schemas) {
	case 0:
		return "", errs.Config("TABLE_NOT_FOUND", "no table "+table)
	case 1:
		return schemas[0], nil
	default:
		return "", errs.Config("TABLE_AMBIGUOUS",
			"table "+table+" exists in: "+strings.Join(schemas, ", ")+"; qualify it as <schema>."+table)
	}
}

// pgType reattaches length/precision to the SQL-standard data_type, which on its
// own drops them (e.g. "character varying", "numeric"). Only char and numeric
// types carry a meaningful modifier — integers report numeric_precision too, but
// "integer(32)" would be noise, so they stay bare.
func pgType(dataType string, charMaxLen, numPrecision, numScale sql.NullInt64) string {
	switch dataType {
	case "character varying", "varchar", "character", "char", "bpchar":
		if charMaxLen.Valid {
			return fmt.Sprintf("%s(%d)", dataType, charMaxLen.Int64)
		}
	case "numeric", "decimal":
		if numPrecision.Valid {
			if numScale.Valid && numScale.Int64 != 0 {
				return fmt.Sprintf("%s(%d,%d)", dataType, numPrecision.Int64, numScale.Int64)
			}
			return fmt.Sprintf("%s(%d)", dataType, numPrecision.Int64)
		}
	}
	return dataType
}

// pgExtra mirrors mysql's "extra" for the pg notions of auto-managed columns:
// GENERATED ... AS IDENTITY and GENERATED ALWAYS AS (expr) STORED.
func pgExtra(identity, generated sql.NullString) string {
	if identity.String == "YES" {
		return "identity"
	}
	if generated.String == "ALWAYS" {
		return "generated"
	}
	return ""
}
