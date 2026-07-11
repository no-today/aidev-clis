package tcli

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// Expr is a parsed assertion: `<lhs> <op> <rhs>` (or `<lhs> exists`).
type Expr struct {
	LHS string
	Op  string // == | != | contains | exists | >= | > | <= | <
	RHS string
}

// exprOps are matched longest-first at a position (so " >= " wins over " > ",
// and " contains " is not shadowed by a comparison op that appears later in a
// quoted value). Spaces are required around the operator.
var exprOps = []string{" contains ", " != ", " == ", " >= ", " <= ", " > ", " < "}

// findOp returns the index + operator of the LEFTMOST operator occurring OUTSIDE
// any quoted region — so `0.message contains "a == b"` splits on contains, not on
// the == inside the quotes.
func findOp(s string) (int, string) {
	inSingle, inDouble, esc := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if esc { // previous char was a backslash inside a quote — take c literally
			esc = false
			continue
		}
		if c == '\\' && (inSingle || inDouble) {
			esc = true
			continue
		}
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case !inSingle && !inDouble:
			for _, op := range exprOps {
				if strings.HasPrefix(s[i:], op) {
					return i, op
				}
			}
		}
	}
	return -1, ""
}

// ParseExpr parses one expression string. Used at validate time and at eval time.
func ParseExpr(s string) (Expr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Expr{}, errs.Config("EXPECT_INVALID", "empty expression")
	}
	if strings.HasSuffix(s, " exists") {
		return Expr{LHS: strings.TrimSpace(strings.TrimSuffix(s, " exists")), Op: "exists"}, nil
	}
	if i, op := findOp(s); i >= 0 {
		return Expr{
			LHS: strings.TrimSpace(s[:i]),
			Op:  strings.TrimSpace(op),
			RHS: strings.TrimSpace(s[i+len(op):]),
		}, nil
	}
	return Expr{}, errs.Config("EXPECT_INVALID", "cannot parse expression: "+s)
}

// EvalExprs renders each expression's {{vars}}, parses it, and evaluates it.
//   - payload != nil: lhs is a gjson path over the payload; the reserved lhs
//     `count` is the length of the collection at collPath (root array fallback).
//   - payload == nil (standalone assertion): the rendered lhs is a literal.
//
// Returns EXPECT_FAILED on the first expression that does not hold.
func EvalExprs(payload []byte, exprs []string, collPath string, vars map[string]string) error {
	for _, raw := range exprs {
		rendered, err := Render(raw, vars)
		if err != nil {
			return err
		}
		e, err := ParseExpr(rendered)
		if err != nil {
			return err
		}
		if err := evalOne(payload, e, collPath); err != nil {
			return err
		}
	}
	return nil
}

func evalOne(payload []byte, e Expr, collPath string) error {
	fail := func(actual string) error {
		return errs.General("EXPECT_FAILED", e.LHS+" "+e.Op+" "+e.RHS+" (got "+actual+")")
	}
	// count: numeric length of the result collection.
	if e.LHS == "count" {
		n := collLen(payload, collPath)
		return numCompare(e, float64(n), strconv.Itoa(n), fail)
	}

	var actual string
	var exists bool
	if payload != nil {
		r := gjson.GetBytes(payload, e.LHS)
		exists, actual = r.Exists(), r.String()
	} else {
		// standalone: lhs is already a rendered literal.
		actual = e.LHS
		exists = strings.TrimSpace(e.LHS) != ""
	}

	switch e.Op {
	case "exists":
		if !exists || strings.TrimSpace(actual) == "" {
			return errs.General("EXPECT_FAILED", e.LHS+" exists (missing/empty)")
		}
		return nil
	case "==":
		if !valEqual(actual, e.RHS) {
			return fail(actual)
		}
	case "!=":
		if valEqual(actual, e.RHS) {
			return fail(actual)
		}
	case "contains":
		if !strings.Contains(actual, unquote(e.RHS)) {
			return fail(actual)
		}
	case ">=", ">", "<=", "<":
		f, err := strconv.ParseFloat(strings.TrimSpace(actual), 64)
		if err != nil {
			return fail(actual)
		}
		return numCompare(e, f, actual, fail)
	default:
		return errs.Config("EXPECT_INVALID", "unknown operator "+e.Op)
	}
	return nil
}

// numCompare compares a numeric lhs against e.RHS using e.Op (==,!=,>=,>,<=,<).
func numCompare(e Expr, lhs float64, actual string, fail func(string) error) error {
	want, err := strconv.ParseFloat(strings.TrimSpace(unquote(e.RHS)), 64)
	if err != nil {
		// non-numeric rhs: fall back to string equality for ==/!=.
		switch e.Op {
		case "==":
			if actual != unquote(e.RHS) {
				return fail(actual)
			}
			return nil
		case "!=":
			if actual == unquote(e.RHS) {
				return fail(actual)
			}
			return nil
		}
		return fail(actual)
	}
	ok := false
	switch e.Op {
	case "==":
		ok = lhs == want
	case "!=":
		ok = lhs != want
	case ">=":
		ok = lhs >= want
	case ">":
		ok = lhs > want
	case "<=":
		ok = lhs <= want
	case "<":
		ok = lhs < want
	}
	if !ok {
		return fail(actual)
	}
	return nil
}

// valEqual compares two scalar strings, numerically when both parse as numbers.
func valEqual(a, b string) bool {
	b = unquote(b)
	if fa, ea := strconv.ParseFloat(a, 64); ea == nil {
		if fb, eb := strconv.ParseFloat(b, 64); eb == nil {
			return fa == fb
		}
	}
	return a == b
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// collLen is the length of the result collection for `count`. Prefers collPath;
// falls back to the root payload when it is itself an array (mongo find / logs).
func collLen(payload []byte, collPath string) int {
	if collPath != "" {
		if r := gjson.GetBytes(payload, collPath); r.IsArray() {
			return len(r.Array())
		}
	}
	if root := gjson.ParseBytes(payload); root.IsArray() {
		return len(root.Array())
	}
	return 0
}

// Extract returns the string value at path over the payload, and whether it
// exists. Used by `capture` (paths are payload-relative, no `data.` prefix).
func Extract(payload []byte, path string) (string, bool) {
	r := gjson.GetBytes(payload, path)
	if !r.Exists() {
		return "", false
	}
	return r.String(), true
}
