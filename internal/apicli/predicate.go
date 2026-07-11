package apicli

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// header looks up a response header case-insensitively. The Headers map is keyed
// by Go-canonical names (built from http.Header), so config casing must not
// matter — `header.x-trace-id` and `header.X-Trace-Id` resolve the same.
func (r *RawResponse) header(name string) (string, bool) {
	v, ok := r.Headers[http.CanonicalHeaderKey(name)]
	return v, ok
}

// RawResponse is the unclassified result of an HTTP call.
type RawResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte

	SetCookies []string

	BodyFile       string
	BodyBytes      int64
	ContentLength  int64
	StreamComplete bool
	SHA256         string
}

// EvalPredicate evaluates a response predicate. Empty expression -> false.
// Grammar: clause ( "||" clause )* ; clause = lhs op rhs ;
// lhs = "status" | "body."path | "header."name ; op = "==" | "!=" ;
// rhs = 'string' | int | null.
//
// The "||" separator and the "==" / "!=" operators are matched only outside
// quoted string literals, so a quoted rhs such as 'a||b' or 'a==b' is treated
// as an opaque value rather than a delimiter/operator (see splitTopLevel and
// findOp).
func EvalPredicate(expr string, r *RawResponse) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, nil
	}
	for _, clause := range splitTopLevel(expr, "||") {
		ok, err := evalClause(strings.TrimSpace(clause), r)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// splitTopLevel splits s on sep, ignoring any sep that falls inside a single- or
// double-quoted string literal. Quote state is tracked with a tiny scanner (no
// escape handling — the predicate grammar has no escapes).
func splitTopLevel(s, sep string) []string {
	var parts []string
	var quote byte // 0 when outside quotes, else the open quote char
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if strings.HasPrefix(s[i:], sep) {
			parts = append(parts, s[start:i])
			i += len(sep) - 1
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// findOp returns the operator ("==" or "!=") and its index — whichever appears
// first outside a quoted literal. idx is -1 when neither operator is present.
func findOp(c string) (op string, idx int) {
	var quote byte
	for i := 0; i < len(c); i++ {
		ch := c[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if strings.HasPrefix(c[i:], "==") {
			return "==", i
		}
		if strings.HasPrefix(c[i:], "!=") {
			return "!=", i
		}
	}
	return "", -1
}

func evalClause(c string, r *RawResponse) (bool, error) {
	op, idx := findOp(c)
	if idx == -1 {
		return false, errs.Config("PREDICATE_INVALID", fmt.Sprintf("cannot parse clause %q", c))
	}
	lhs, isNull := resolveLHS(strings.TrimSpace(c[:idx]), r)
	rhs, rhsNull := parseRHS(strings.TrimSpace(c[idx+len(op):]))
	var equal bool
	switch {
	case rhsNull:
		equal = isNull
	default:
		equal = !isNull && lhs == rhs
	}
	if op == "!=" {
		return !equal, nil
	}
	return equal, nil
}

// resolveLHS returns the string value of the signal and whether it is absent.
func resolveLHS(lhs string, r *RawResponse) (string, bool) {
	switch {
	case lhs == "status":
		return strconv.Itoa(r.Status), false
	case strings.HasPrefix(lhs, "header."):
		v, ok := r.header(strings.TrimPrefix(lhs, "header."))
		return v, !ok
	case strings.HasPrefix(lhs, "body."):
		res := gjson.GetBytes(r.Body, strings.TrimPrefix(lhs, "body."))
		return res.String(), !res.Exists()
	}
	return "", true
}

// parseRHS strips quotes; recognizes the bare token null.
func parseRHS(rhs string) (val string, isNull bool) {
	if rhs == "null" {
		return "", true
	}
	if len(rhs) >= 2 && (rhs[0] == '\'' || rhs[0] == '"') && rhs[len(rhs)-1] == rhs[0] {
		return rhs[1 : len(rhs)-1], false
	}
	return rhs, false
}
