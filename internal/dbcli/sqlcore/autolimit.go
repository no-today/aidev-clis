package sqlcore

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// DefaultRowCap is the LIMIT appended to a read with no explicit LIMIT.
// MaxLimit is the largest an explicit LIMIT may be — beyond it the query is
// rejected (not silently truncated, which would mislead). For more rows,
// paginate with OFFSET or aggregate.
const (
	DefaultRowCap = 100
	MaxLimit      = 100
)

var (
	// captures `LIMIT n` and MySQL's `LIMIT offset, count` (group 2 = count).
	limitRe = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*(?:,\s*(\d+))?`)
	fetchRe = regexp.MustCompile(`(?i)\bFETCH\s+(?:FIRST|NEXT)\s+(\d+)\s+ROWS?\b`)
)

// ApplyAutoLimit bounds a read query (SELECT/WITH):
//   - no top-level LIMIT → append ` LIMIT 100`;
//   - explicit top-level LIMIT/FETCH ≤ MaxLimit → kept as written;
//   - explicit > MaxLimit → error LIMIT_TOO_LARGE (the query is not run).
//
// Non-reads are returned unchanged. A subquery LIMIT (depth > 0) is ignored.
func ApplyAutoLimit(sql string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")
	if Classify(trimmed) != ClassRead {
		return sql, nil
	}
	first := strings.ToUpper(firstToken(strings.ToUpper(stripLiterals(trimmed))))
	if first != "SELECT" && first != "WITH" {
		return sql, nil
	}
	n, ok := topLevelLimit(stripLiterals(trimmed))
	if !ok {
		return trimmed + " LIMIT " + strconv.Itoa(DefaultRowCap), nil
	}
	if n > MaxLimit {
		return "", errs.Config("LIMIT_TOO_LARGE",
			fmt.Sprintf("LIMIT %d exceeds the maximum %d; paginate with `LIMIT %d OFFSET <n>` or aggregate", n, MaxLimit, MaxLimit))
	}
	return trimmed, nil
}

// topLevelLimit returns the value of the first depth-0 LIMIT, else the first
// depth-0 FETCH FIRST/NEXT (a query has at most one), or ok=false if neither.
func topLevelLimit(stripped string) (int, bool) {
	for _, m := range limitRe.FindAllStringSubmatchIndex(stripped, -1) {
		if parenDepth(stripped[:m[0]]) == 0 {
			// MySQL `LIMIT offset, count`: the row count is the SECOND operand.
			if m[4] >= 0 {
				n, _ := strconv.Atoi(stripped[m[4]:m[5]])
				return n, true
			}
			n, _ := strconv.Atoi(stripped[m[2]:m[3]])
			return n, true
		}
	}
	for _, m := range fetchRe.FindAllStringSubmatchIndex(stripped, -1) {
		if parenDepth(stripped[:m[0]]) == 0 {
			n, _ := strconv.Atoi(stripped[m[2]:m[3]])
			return n, true
		}
	}
	return 0, false
}

func parenDepth(s string) int {
	d := 0
	for _, c := range s {
		switch c {
		case '(':
			d++
		case ')':
			if d > 0 {
				d--
			}
		}
	}
	return d
}
