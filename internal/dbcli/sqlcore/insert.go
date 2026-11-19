package sqlcore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// flavor captures the small per-engine differences in INSERT literal syntax.
type flavor struct {
	identQuote   byte   // '`' (mysql) or '"' (standard)
	escBackslash bool   // mysql treats '\' as an escape, so double it
	boolTrue     string // "1" (mysql) / "TRUE"
	boolFalse    string // "0" (mysql) / "FALSE"
	blobPrefix   string // "0x" (mysql) / "'\\x"
	blobSuffix   string // ""   (mysql) / "'"
}

// flavorFor selects the literal flavor from a Dialect.DriverName(). Only the
// mysql driver uses backtick identifiers + backslash escaping + 0x blobs + 1/0
// bools; pgx/sqlite/anything-else use the SQL-standard flavor.
func flavorFor(driverName string) flavor {
	if driverName == "mysql" {
		return flavor{identQuote: '`', escBackslash: true, boolTrue: "1", boolFalse: "0", blobPrefix: "0x", blobSuffix: ""}
	}
	return flavor{identQuote: '"', escBackslash: false, boolTrue: "TRUE", boolFalse: "FALSE", blobPrefix: `'\x`, blobSuffix: "'"}
}

func (f flavor) quoteIdent(name string) string {
	q := string(f.identQuote)
	return q + strings.ReplaceAll(name, q, q+q) + q
}

// quoteTable quotes each dotted part of a (possibly schema-qualified) name,
// stripping any quote chars the source already wrapped a part in.
func (f flavor) quoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i, p := range parts {
		parts[i] = f.quoteIdent(strings.Trim(p, "`\""))
	}
	return strings.Join(parts, ".")
}

func (f flavor) quoteString(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	if f.escBackslash {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
	return "'" + s + "'"
}

// renderValue renders one scanned cell as a SQL literal. It works on raw
// database/sql values (NOT Coerce output) so nothing is lossy or truncated.
func (f flavor) renderValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if x {
			return f.boolTrue
		}
		return f.boolFalse
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case string:
		return f.quoteString(x)
	case []byte:
		// Text columns commonly arrive as []byte (esp. MySQL); valid UTF-8
		// round-trips as a quoted string. Only true binary (non-UTF-8) becomes
		// a hex blob literal, whose syntax is flavor-specific.
		if utf8.Valid(x) {
			return f.quoteString(string(x))
		}
		return f.blobPrefix + hex.EncodeToString(x) + f.blobSuffix
	case time.Time:
		return f.quoteString(x.UTC().Format(time.RFC3339))
	default:
		if s, ok := v.(fmt.Stringer); ok {
			return f.quoteString(s.String())
		}
		return f.quoteString(fmt.Sprint(v))
	}
}

// fromRe matches a FROM keyword and the whitespace after it.
var fromRe = regexp.MustCompile(`(?i)\bFROM\s+`)

// clauseRe marks where the FROM list ends (a depth-0 clause keyword).
var clauseRe = regexp.MustCompile(`(?i)\b(WHERE|GROUP|ORDER|HAVING|LIMIT|OFFSET|FETCH|UNION|WINDOW)\b`)

// joinRe finds a JOIN inside the FROM list (ambiguous → refuse inference).
var joinRe = regexp.MustCompile(`(?i)\bJOIN\b`)

// parseInsertArgs splits the verb args (args[0]=="insert") into an optional
// --table override and the trailing SQL text.
func parseInsertArgs(args []string) (table, sqlText string) {
	var parts []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--table" && i+1 < len(args):
			table = args[i+1]
			i++
		case strings.HasPrefix(a, "--table="):
			table = strings.TrimPrefix(a, "--table=")
		default:
			parts = append(parts, a)
		}
	}
	return table, strings.Join(parts, " ")
}

// inferTable extracts the single source table from a SELECT's FROM clause.
// It refuses (errs INSERT_NO_TABLE) on no FROM, a subquery, joined tables, or
// comma-separated tables — the caller should then require --table. Literals are
// blanked for keyword scanning via stripLiterals, which is length-preserving so
// offsets map 1:1 back onto the original text.
func inferTable(sqlText string) (string, error) {
	s := stripLiterals(sqlText)
	// locate the first depth-0 FROM
	fromEnd := -1
	for _, m := range fromRe.FindAllStringIndex(s, -1) {
		if parenDepth(s[:m[0]]) == 0 {
			fromEnd = m[1]
			break
		}
	}
	if fromEnd == -1 {
		return "", errNoTable()
	}
	// bound the FROM list at the next depth-0 clause keyword (or end)
	listEnd := len(s)
	for _, m := range clauseRe.FindAllStringIndex(s[fromEnd:], -1) {
		if parenDepth(s[:fromEnd+m[0]]) == 0 {
			listEnd = fromEnd + m[0]
			break
		}
	}
	region := s[fromEnd:listEnd]
	// subquery (derived table): first non-space is '('
	if strings.HasPrefix(strings.TrimSpace(region), "(") {
		return "", errNoTable()
	}
	// joined or comma-listed tables → ambiguous
	if joinRe.MatchString(region) || strings.Contains(region, ",") {
		return "", errNoTable()
	}
	// the table is the first token; read it from the ORIGINAL text (offsets align)
	tok := firstSQLToken(sqlText[fromEnd:listEnd])
	if tok == "" {
		return "", errNoTable()
	}
	return tok, nil
}

func errNoTable() error {
	return errs.Config("INSERT_NO_TABLE",
		"could not infer the target table from the FROM clause; pass --table <name>")
}

// firstSQLToken returns the leading identifier token (letters/digits/_/$ plus
// '.' for schema.table and the quote chars), stopping at whitespace, comma, or
// a paren. Leading whitespace is skipped.
func firstSQLToken(s string) string {
	s = strings.TrimLeft(s, " \t\r\n")
	end := len(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',' || c == '(' || c == ')' {
			end = i
			break
		}
	}
	return strings.TrimSpace(s[:end])
}

// runInsert renders a read query's rows as INSERT statements written verbatim
// through out's RawOutput capability. Read-only: writes/DDL are refused.
func runInsert(ctx context.Context, d Dialect, db *sql.DB, in dbcli.Input, out dbcli.Output) error {
	raw, ok := out.(dbcli.RawOutput)
	if !ok {
		return errs.General("OUTPUT_MISUSE", "insert verb requires raw output")
	}
	table, sqlText := parseInsertArgs(in.Args)
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return errs.Config("EMPTY_SQL", "no SQL provided")
	}
	if HasMultipleStatements(sqlText) {
		return errs.Config("MULTI_STATEMENT", "multiple statements are not allowed; run one statement at a time")
	}
	switch Classify(sqlText) {
	case ClassDDL:
		return errs.Config("DDL_REFUSED", "DDL (CREATE/DROP/ALTER/TRUNCATE/GRANT/REVOKE) is never allowed")
	case ClassHazard:
		return errs.Config("HAZARD_REFUSED", "server-side file-write / command-exec (INTO OUTFILE/DUMPFILE, COPY ... TO PROGRAM) is never allowed")
	case ClassWrite:
		return errs.Config("WRITE_NOT_ALLOWED", "insert renders a read query; the SQL must be a SELECT")
	}
	if table == "" {
		t, err := inferTable(sqlText)
		if err != nil {
			return err
		}
		table = t
	}
	limited, err := ApplyAutoLimit(sqlText)
	if err != nil {
		return err
	}
	fl := flavorFor(d.DriverName())

	// Unlike runSQL, insert skips the BackendID/watchCancel KILL-on-cancel setup:
	// the read is auto-LIMITed (≤100 rows by default), so a cancel closing the
	// conn is enough — no runaway backend to KILL.
	//
	// Pin one conn so USE + the statement share a session (mirrors runSQL). If a
	// dedicated conn can't be acquired, fall back to the pool — best-effort, so
	// --database is then NOT applied (same degraded mode as runSQL).
	conn, err := db.Conn(ctx)
	if err != nil {
		return renderInsert(ctx, db, limited, table, fl, raw)
	}
	defer conn.Close()
	if in.Database != "" {
		if err := d.UseDatabase(ctx, conn, in.Database); err != nil {
			return err
		}
	}
	return renderInsert(ctx, conn, limited, table, fl, raw)
}

// renderInsert runs the read and writes one INSERT statement per row.
func renderInsert(ctx context.Context, q queryer, sqlText, table string, fl flavor, raw dbcli.RawOutput) error {
	rows, err := q.QueryContext(ctx, sqlText)
	if err != nil {
		return wrapDBErr(ctx, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return errs.Remote("DB_COLUMNS", err.Error())
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = fl.quoteIdent(c)
	}
	prefix := "INSERT INTO " + fl.quoteTable(table) + " (" + strings.Join(quoted, ", ") + ") VALUES ("
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return errs.Remote("DB_SCAN", err.Error())
		}
		vals := make([]string, len(cells))
		for i, c := range cells {
			vals[i] = fl.renderValue(c)
		}
		if err := raw.Raw(prefix + strings.Join(vals, ", ") + ");\n"); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return wrapDBErr(ctx, err)
	}
	return nil
}
