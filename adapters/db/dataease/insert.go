package dataease

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// runInsert renders a SELECT's rows as INSERT statements, written verbatim
// through out's RawOutput capability.
//
// Unlike the SQL drivers' insert (sqlcore), DataEase returns every cell as a
// string or null — there is no type information — so every non-null value is
// emitted as a MySQL-quoted string literal (MySQL coerces these into numeric/
// date columns on insert; these DataEase backends are MySQL). NULL stays NULL.
func runInsert(ctx context.Context, cfg *Config, in dbcli.Input, out dbcli.Output) error {
	raw, ok := out.(dbcli.RawOutput)
	if !ok {
		return errs.General("OUTPUT_MISUSE", "insert verb requires raw output")
	}
	table, exclude, sqlText := parseInsertArgs(in.Args)
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return errs.Config("EMPTY_SQL", "no SQL provided")
	}
	// insert renders a read query's rows; route the SQL through the same local
	// read-only gate as a plain query so a write/DDL/multi-statement is rejected
	// here (shared error codes) before any HTTP call.
	if err := guardReadOnly(sqlText); err != nil {
		return err
	}
	if table == "" {
		t, err := inferTable(sqlText)
		if err != nil {
			return err
		}
		table = t
	}
	result, err := queryWithAutoLogin(ctx, cfg, sqlText)
	if err != nil {
		return err
	}
	return renderInsert(result, table, exclude, raw)
}

// parseInsertArgs splits the verb args (args[0]=="insert") into an optional
// --table override, the set of columns to drop (--exclude a,b,c, case-insensitive,
// repeatable), and the trailing SQL text.
func parseInsertArgs(args []string) (table string, exclude map[string]bool, sqlText string) {
	exclude = map[string]bool{}
	addExcluded := func(csv string) {
		for _, c := range strings.Split(csv, ",") {
			if c = strings.TrimSpace(c); c != "" {
				exclude[strings.ToLower(c)] = true
			}
		}
	}
	var parts []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--table" && i+1 < len(args):
			table = args[i+1]
			i++
		case strings.HasPrefix(a, "--table="):
			table = strings.TrimPrefix(a, "--table=")
		case a == "--exclude" && i+1 < len(args):
			addExcluded(args[i+1])
			i++
		case strings.HasPrefix(a, "--exclude="):
			addExcluded(strings.TrimPrefix(a, "--exclude="))
		default:
			parts = append(parts, a)
		}
	}
	return table, exclude, strings.Join(parts, " ")
}

var fromTableRe = regexp.MustCompile(`(?is)\bfrom\s+([^\s,()]+)`)
var hasJoinRe = regexp.MustCompile(`(?is)\bjoin\b`)

// inferTable extracts the single source table from a SELECT's FROM clause. It
// refuses (INSERT_NO_TABLE) when the source is ambiguous — a JOIN or a derived
// subquery — so the caller must then pass --table.
func inferTable(sqlText string) (string, error) {
	if hasJoinRe.MatchString(sqlText) {
		return "", errNoTable()
	}
	m := fromTableRe.FindStringSubmatch(sqlText)
	if m == nil || m[1] == "" || m[1] == "(" {
		return "", errNoTable()
	}
	return m[1], nil
}

func errNoTable() error {
	return errs.Config("INSERT_NO_TABLE",
		"could not infer the target table from the FROM clause; pass --table <name>")
}

// renderInsert writes one INSERT statement per result row (MySQL flavor),
// dropping any column whose name (case-insensitive) is in exclude.
func renderInsert(result *dbcli.Result, table string, exclude map[string]bool, raw dbcli.RawOutput) error {
	var keep []int
	var quoted []string
	for i, c := range result.Columns {
		if exclude[strings.ToLower(c)] {
			continue
		}
		keep = append(keep, i)
		quoted = append(quoted, quoteIdent(c))
	}
	if len(keep) == 0 {
		return errs.Config("INSERT_NO_COLUMNS", "all columns were excluded; nothing to insert")
	}
	prefix := "INSERT INTO " + quoteTable(table) + " (" + strings.Join(quoted, ", ") + ") VALUES ("
	for _, row := range result.Rows {
		vals := make([]string, 0, len(keep))
		for _, i := range keep {
			vals = append(vals, renderValue(row[i]))
		}
		if err := raw.Raw(prefix + strings.Join(vals, ", ") + ");\n"); err != nil {
			return err
		}
	}
	return nil
}

// quoteIdent backtick-quotes a MySQL identifier.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// quoteTable backtick-quotes each dotted part of a (possibly schema-qualified)
// name, stripping any quote chars the source already wrapped a part in.
func quoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i, p := range parts {
		parts[i] = quoteIdent(strings.Trim(p, "`\""))
	}
	return strings.Join(parts, ".")
}

// renderValue renders a DataEase cell (string or nil) as a MySQL literal.
func renderValue(v any) string {
	if v == nil {
		return "NULL"
	}
	s, ok := v.(string)
	if !ok {
		// DataEase normally returns strings; fall back to a string form for any
		// number/bool the JSON decoder produced.
		s = fmt.Sprint(v)
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}
