package sqlcore

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/diag"
	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// cellCap is the per-cell character cap.
const cellCap = 256

var verbs = map[string]bool{"databases": true, "tables": true, "describe": true, "doctor": true, "insert": true}

// Run executes one dbcli invocation against an already-opened *sql.DB using the
// engine's Dialect, and emits the result via out.Batch (exactly once).
func Run(ctx context.Context, d Dialect, db *sql.DB, in dbcli.Input, out dbcli.Output) error {
	verb := ""
	if len(in.Args) > 0 {
		verb = strings.ToLower(in.Args[0])
	}
	if verbs[verb] {
		return runVerb(ctx, d, db, verb, in, out)
	}
	return runSQL(ctx, d, db, strings.Join(in.Args, " "), in, out)
}

func runSQL(ctx context.Context, d Dialect, db *sql.DB, sqlText string, in dbcli.Input, out dbcli.Output) error {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return errs.Config("EMPTY_SQL", "no SQL provided")
	}
	if HasMultipleStatements(sqlText) {
		return errs.Config("MULTI_STATEMENT", "multiple statements are not allowed; run one statement at a time")
	}
	class := Classify(sqlText)
	if class == ClassDDL {
		return errs.Config("DDL_REFUSED", "DDL (CREATE/DROP/ALTER/TRUNCATE/GRANT/REVOKE) is never allowed")
	}
	if class == ClassHazard {
		return errs.Config("HAZARD_REFUSED", "server-side file-write / command-exec (INTO OUTFILE/DUMPFILE, COPY ... TO PROGRAM) is never allowed")
	}
	if class == ClassWrite && !in.AllowWrite {
		return errs.Config("WRITE_NOT_ALLOWED", "this statement writes; pass --allow-write to permit it")
	}

	// Pin ONE connection for USE + backend-id + the statement so they share a
	// session and the KILL targets the right backend. Fall back to the pool if a
	// dedicated conn can't be acquired (KILL/--database then best-effort).
	connStart := time.Now()
	conn, err := db.Conn(ctx)
	if err != nil {
		return executeStatement(ctx, db, class, sqlText, out)
	}
	defer conn.Close()
	diag.From(ctx).Logf(1, "connected in %s", time.Since(connStart).Round(time.Microsecond))
	if in.Database != "" {
		if err := d.UseDatabase(ctx, conn, in.Database); err != nil {
			return err
		}
	}
	if id, e := d.BackendID(ctx, conn); e == nil && id != "" {
		stop := watchCancel(ctx, d, db, id) // KILL issued on a SEPARATE pool conn
		defer stop()
	}
	return executeStatement(ctx, conn, class, sqlText, out)
}

// executeStatement runs the (already-guarded) statement on q and emits the
// result. Reads get an auto-LIMIT (default 100; an explicit LIMIT >100 errors).
func executeStatement(ctx context.Context, q queryer, class Class, sqlText string, out dbcli.Output) error {
	dg := diag.From(ctx)
	if class == ClassWrite {
		dg.Logf(2, "final SQL: %s", sqlText)
		start := time.Now()
		n, err := execStmt(ctx, q, sqlText)
		if err != nil {
			return err
		}
		dg.Logf(1, "executed in %s; affected %d row(s)", time.Since(start).Round(time.Microsecond), n)
		return out.Batch(map[string]any{"affected": n})
	}
	limited, err := ApplyAutoLimit(sqlText)
	if err != nil {
		return err
	}
	dg.Logf(2, "final SQL: %s", limited) // the statement actually sent, incl. any auto-LIMIT
	start := time.Now()
	res, err := queryRows(ctx, q, limited)
	if err != nil {
		return err
	}
	dg.Logf(1, "query executed in %s; returned %d row(s)", time.Since(start).Round(time.Microsecond), len(res.Rows))
	var warnings []string
	if TruncateCells(res.Rows, cellCap) {
		warnings = append(warnings, "cell(s) truncated to 256 chars")
	}
	return out.Batch(res, warnings...)
}

func runVerb(ctx context.Context, d Dialect, db *sql.DB, verb string, in dbcli.Input, out dbcli.Output) error {
	switch verb {
	case "databases":
		names, err := d.ListDatabases(ctx, db)
		if err != nil {
			return err
		}
		rows := make([][]any, 0, len(names))
		for _, n := range names {
			rows = append(rows, []any{n})
		}
		return out.Batch(dbcli.Result{Columns: []string{"database"}, Rows: rows})
	case "tables":
		database, like := in.Database, ""
		for i := 1; i < len(in.Args); i++ {
			switch {
			case in.Args[i] == "--like" && i+1 < len(in.Args):
				like = in.Args[i+1]
				i++
			case !strings.HasPrefix(in.Args[i], "-"):
				database = in.Args[i]
			}
		}
		tbls, err := d.ListTables(ctx, db, database, like)
		if err != nil {
			return err
		}
		rows := make([][]any, 0, len(tbls))
		for _, t := range tbls {
			rows = append(rows, []any{t.Database, t.Name})
		}
		return out.Batch(dbcli.Result{Columns: []string{"database", "name"}, Rows: rows})
	case "describe":
		if len(in.Args) < 2 {
			return errs.Config("DESCRIBE_NO_TABLE", "describe requires a table name")
		}
		database, table := splitQualified(in.Args[1], in.Database)
		ts, err := d.Describe(ctx, db, database, table)
		if err != nil {
			return err
		}
		return out.Batch(ts)
	case "doctor":
		if err := db.PingContext(ctx); err != nil {
			return errs.Remote("DB_PING", err.Error())
		}
		return out.Batch(map[string]any{"ok": true})
	case "insert":
		return runInsert(ctx, d, db, in, out)
	}
	return errs.Config("UNKNOWN_VERB", "unknown verb "+verb)
}

// splitQualified splits "schema.table" into (schema, table); a bare name uses
// the fallback database.
func splitQualified(arg, fallback string) (string, string) {
	if i := strings.IndexByte(arg, '.'); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return fallback, arg
}
