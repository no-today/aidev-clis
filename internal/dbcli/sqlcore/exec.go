package sqlcore

import (
	"context"
	"database/sql"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// queryer is satisfied by both *sql.DB and *sql.Conn.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// queryRows runs a read and returns ordered columns + coerced rows.
func queryRows(ctx context.Context, q queryer, query string) (dbcli.Result, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return dbcli.Result{}, wrapDBErr(ctx, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return dbcli.Result{}, errs.Remote("DB_COLUMNS", err.Error())
	}
	res := dbcli.Result{Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return dbcli.Result{}, errs.Remote("DB_SCAN", err.Error())
		}
		for i := range cells {
			cells[i] = Coerce(cells[i])
		}
		res.Rows = append(res.Rows, cells)
	}
	if err := rows.Err(); err != nil {
		return dbcli.Result{}, wrapDBErr(ctx, err)
	}
	return res, nil
}

// execStmt runs a write and returns rows affected.
func execStmt(ctx context.Context, q queryer, stmt string) (int64, error) {
	r, err := q.ExecContext(ctx, stmt)
	if err != nil {
		return 0, wrapDBErr(ctx, err)
	}
	n, err := r.RowsAffected()
	if err != nil {
		return 0, nil // some drivers don't report it; not fatal
	}
	return n, nil
}

// wrapDBErr classifies a driver error: a cancelled/expired context → Timeout,
// otherwise Remote.
func wrapDBErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errs.Timeout("DB_TIMEOUT", ctx.Err().Error())
	}
	return errs.Remote("DB_QUERY", err.Error())
}
