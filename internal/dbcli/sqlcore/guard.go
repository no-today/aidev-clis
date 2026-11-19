// Package sqlcore is dbcli's shared SQL engine: statement guard, auto-LIMIT,
// cell truncation, type coercion, and the Dialect-driven Run dispatch. Used by
// the mysql/pg/kingbase adapters; names no concrete driver.
package sqlcore

import (
	"regexp"
	"strings"
)

// Class is the security class of a statement.
type Class int

const (
	ClassRead   Class = iota // SELECT/SHOW/EXPLAIN/DESCRIBE/WITH (pure read)
	ClassWrite               // DML + pseudo-read side effects + unknown
	ClassDDL                 // CREATE/DROP/ALTER/... (refused even with --allow-write)
	ClassHazard              // server-side file-write / command-exec (refused even with --allow-write)
)

var (
	ddlFirst   = set("CREATE", "DROP", "ALTER", "TRUNCATE", "GRANT", "REVOKE", "RENAME")
	readFirst  = set("SELECT", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "WITH")
	writeFirst = set("INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE", "CALL", "LOAD", "COPY")

	// pseudo-read side effects: a SELECT/WITH containing any of these is a write.
	hazard = regexp.MustCompile(`(?i)\b(INTO\b|FOR\s+UPDATE\b|FOR\s+SHARE\b|nextval\s*\(|setval\s*\(|pg_sleep\s*\(|pg_advisory_lock|pg_advisory_xact_lock|pg_terminate_backend|pg_cancel_backend|lo_export\s*\(|load_file\s*\()`)
	// server-side file-write / command-exec primitives: `SELECT ... INTO
	// OUTFILE/DUMPFILE` (mysql writes a server file) and `COPY ... TO PROGRAM`
	// (pg execs a shell command). These escape the database entirely, so they are
	// refused unconditionally — never unlocked by --allow-write, like DDL.
	refuse = regexp.MustCompile(`(?i)\bINTO\s+(OUTFILE|DUMPFILE)\b|\bTO\s+PROGRAM\b`)
	// a WITH whose body mutates.
	cteWrite = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|TRUNCATE|DROP|ALTER|CREATE|GRANT|REVOKE)\b`)
	// EXPLAIN ANALYZE actually executes the plan.
	analyzeRe = regexp.MustCompile(`(?i)\bANALYZE\b`)
)

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Classify returns the security class of the (first) statement.
func Classify(sql string) Class {
	s := strings.TrimSpace(stripLiterals(sql))
	upper := strings.ToUpper(s)
	first := firstToken(upper)
	// Hazard check first: a file-write/command-exec primitive is refused whatever
	// the leading keyword (SELECT ... INTO OUTFILE, COPY ... TO PROGRAM).
	if refuse.MatchString(s) {
		return ClassHazard
	}
	switch {
	case ddlFirst[first]:
		return ClassDDL
	// EXPLAIN plans without executing, so the explained statement's own keywords
	// (INSERT, INTO, ...) are irrelevant — EXPLAIN <anything> is a pure read.
	// EXPLAIN ANALYZE is the exception: it executes the plan.
	case first == "EXPLAIN":
		if analyzeRe.MatchString(upper) {
			return ClassWrite
		}
		return ClassRead
	case first == "WITH" && cteWrite.MatchString(upper):
		return ClassWrite
	case readFirst[first]:
		if hazard.MatchString(s) {
			return ClassWrite
		}
		return ClassRead
	case writeFirst[first]:
		return ClassWrite
	default:
		return ClassWrite // unknown (SET/USE/BEGIN/...) — needs --allow-write, not DDL-refused
	}
}

// HasMultipleStatements reports whether the SQL contains more than one top-level
// statement (a ';' with non-whitespace after it), ignoring literals/comments.
// Prevents `SELECT 1; DROP TABLE t` smuggling past a single-statement classify.
func HasMultipleStatements(sql string) bool {
	s := stripLiterals(sql)
	if i := strings.IndexByte(s, ';'); i >= 0 {
		return strings.TrimSpace(s[i+1:]) != ""
	}
	return false
}

func firstToken(s string) string {
	s = strings.TrimLeft(s, "( \t\r\n") // a leading "(SELECT ...)" is still a SELECT
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' {
			return s[:i]
		}
	}
	return s
}

// stripLiterals blanks out string literals (single/double/backtick + pg
// dollar-quoting) and comments so keyword scanning sees only code. It is
// LENGTH-PRESERVING: every blanked byte becomes a space, so byte offsets into
// the result map 1:1 onto the input (auto-LIMIT relies on this). MySQL
// executable comments `/*!...*/` and optimizer hints `/*+...*/` are NOT dead —
// the engine runs their body — so only the delimiters are blanked, exposing the
// body to the guard. (Block comments terminate at the first `*/`, matching
// MySQL; pg's rare nested comments fail safe — extra code reads as a write.)
func stripLiterals(sql string) string {
	b := []byte(sql)
	out := append([]byte(nil), b...)
	blank := func(lo, hi int) {
		for k := lo; k < hi; k++ {
			out[k] = ' '
		}
	}
	for i := 0; i < len(b); {
		switch {
		case b[i] == '\'' || b[i] == '"' || b[i] == '`':
			j := scanQuoted(b, i)
			blank(i, j)
			i = j
		case b[i] == '$':
			if j, ok := scanDollar(b, i); ok {
				blank(i, j)
				i = j
			} else {
				i++
			}
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := scanBlockComment(b, i)
			if i+2 < len(b) && (b[i+2] == '!' || b[i+2] == '+') {
				blank(i, i+3) // "/*!" or "/*+"
				if j >= i+5 {
					blank(j-2, j) // closing "*/"
				}
			} else {
				blank(i, j)
			}
			i = j
		default:
			i++
		}
	}
	return string(out)
}

// scanQuoted returns the index just past a '/"/` literal beginning at i,
// honoring doubled-quote escapes and (for ' and ") backslash escapes. An
// unterminated literal runs to EOF (so its content is fully blanked — the
// engine also rejects it as a syntax error).
func scanQuoted(b []byte, i int) int {
	q := b[i]
	for j := i + 1; j < len(b); {
		switch {
		case b[j] == q:
			if j+1 < len(b) && b[j+1] == q {
				j += 2
				continue
			}
			return j + 1
		case b[j] == '\\' && q != '`' && j+1 < len(b):
			j += 2
		default:
			j++
		}
	}
	return len(b)
}

// scanDollar handles pg/kingbase dollar-quoting `$tag$ ... $tag$` (tag may be
// empty: `$$ ... $$`). Returns (end, true) past the closing tag, or (_, false)
// if i does not open a dollar-quote (e.g. a `$1` positional parameter).
func scanDollar(b []byte, i int) (int, bool) {
	k := i + 1
	for k < len(b) && isIdentByte(b[k]) {
		k++
	}
	if k >= len(b) || b[k] != '$' {
		return 0, false
	}
	tag := b[i : k+1] // includes both '$'
	if idx := bytesIndexFrom(b, k+1, tag); idx >= 0 {
		return idx + len(tag), true
	}
	return len(b), true // unterminated → to EOF
}

// scanBlockComment returns the index just past the first `*/` after a `/*` at i,
// or EOF if unterminated.
func scanBlockComment(b []byte, i int) int {
	for j := i + 2; j+1 < len(b); j++ {
		if b[j] == '*' && b[j+1] == '/' {
			return j + 2
		}
	}
	return len(b)
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func bytesIndexFrom(b []byte, from int, sub []byte) int {
	for j := from; j+len(sub) <= len(b); j++ {
		if string(b[j:j+len(sub)]) == string(sub) {
			return j
		}
	}
	return -1
}
