package tcli

import (
	"strings"
	"testing"
)

// ——————————————————————————————————————————
// ParseExpr
// ——————————————————————————————————————————

func TestParseExpr_AllOperators(t *testing.T) {
	cases := []struct {
		in  string
		op  string
		lhs string
		rhs string
	}{
		{"status_code == 200", "==", "status_code", "200"},
		{"status_code != 500", "!=", "status_code", "500"},
		{"count >= 1", ">=", "count", "1"},
		{"count > 0", ">", "count", "0"},
		{"count <= 10", "<=", "count", "10"},
		{"count < 5", "<", "count", "5"},
		{"message contains error", "contains", "message", "error"},
		{"body.id exists", "exists", "body.id", ""},
	}
	for _, tc := range cases {
		e, err := ParseExpr(tc.in)
		if err != nil {
			t.Errorf("%q: unexpected parse error: %v", tc.in, err)
			continue
		}
		if e.Op != tc.op {
			t.Errorf("%q: op=%q want %q", tc.in, e.Op, tc.op)
		}
		if e.LHS != tc.lhs {
			t.Errorf("%q: lhs=%q want %q", tc.in, e.LHS, tc.lhs)
		}
		if tc.op != "exists" && e.RHS != tc.rhs {
			t.Errorf("%q: rhs=%q want %q", tc.in, e.RHS, tc.rhs)
		}
	}
}

func TestParseExpr_Errors(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"status_code200", // no operator, no spaces around it
		"x BADOP y",      // unrecognised word without spaces-as-in-supported-ops
	}
	for _, b := range bad {
		if _, err := ParseExpr(b); err == nil {
			t.Errorf("%q: expected parse error, got none", b)
		}
	}
}

func TestParseExpr_LongestOpFirst(t *testing.T) {
	// ">=" must win over ">"
	e, err := ParseExpr("count >= 5")
	if err != nil || e.Op != ">=" {
		t.Fatalf("expected >= operator, got %q err %v", e.Op, err)
	}
	// "<=" must win over "<"
	e, err = ParseExpr("count <= 5")
	if err != nil || e.Op != "<=" {
		t.Fatalf("expected <= operator, got %q err %v", e.Op, err)
	}
	// "!=" must win over "="
	e, err = ParseExpr("x != y")
	if err != nil || e.Op != "!=" {
		t.Fatalf("expected != operator, got %q err %v", e.Op, err)
	}
}

// ——————————————————————————————————————————
// EvalExprs — payload-based (JSON path)
// ——————————————————————————————————————————

var samplePayload = []byte(`{
  "status_code": 200,
  "body": {
    "id": "ORD-1",
    "amount": 99,
    "message": "order created successfully"
  }
}`)

func TestEvalExprs_EqualNumeric(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"status_code == 200"}, "", nil); err != nil {
		t.Fatalf("numeric == failed: %v", err)
	}
}

func TestEvalExprs_EqualString(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.id == ORD-1"}, "", nil); err != nil {
		t.Fatalf("string == failed: %v", err)
	}
}

func TestEvalExprs_NotEqual(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"status_code != 500"}, "", nil); err != nil {
		t.Fatalf("!= failed: %v", err)
	}
}

func TestEvalExprs_Contains(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.message contains created"}, "", nil); err != nil {
		t.Fatalf("contains failed: %v", err)
	}
}

func TestEvalExprs_ContainsFail(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.message contains NOPE"}, "", nil); err == nil {
		t.Fatal("contains should fail when substring absent")
	}
}

func TestEvalExprs_Exists(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.id exists"}, "", nil); err != nil {
		t.Fatalf("exists failed: %v", err)
	}
}

func TestEvalExprs_ExistsMissing(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.nonexistent exists"}, "", nil); err == nil {
		t.Fatal("exists should fail when path absent")
	}
}

func TestEvalExprs_GTE(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.amount >= 99"}, "", nil); err != nil {
		t.Fatalf(">= failed: %v", err)
	}
	if err := EvalExprs(samplePayload, []string{"body.amount >= 100"}, "", nil); err == nil {
		t.Fatal(">= should fail: 99 < 100")
	}
}

func TestEvalExprs_GT(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.amount > 50"}, "", nil); err != nil {
		t.Fatalf("> failed: %v", err)
	}
	if err := EvalExprs(samplePayload, []string{"body.amount > 99"}, "", nil); err == nil {
		t.Fatal("> should fail: 99 not > 99")
	}
}

func TestEvalExprs_LTE(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.amount <= 99"}, "", nil); err != nil {
		t.Fatalf("<= failed: %v", err)
	}
	if err := EvalExprs(samplePayload, []string{"body.amount <= 50"}, "", nil); err == nil {
		t.Fatal("<= should fail: 99 > 50")
	}
}

func TestEvalExprs_LT(t *testing.T) {
	if err := EvalExprs(samplePayload, []string{"body.amount < 200"}, "", nil); err != nil {
		t.Fatalf("< failed: %v", err)
	}
	if err := EvalExprs(samplePayload, []string{"body.amount < 10"}, "", nil); err == nil {
		t.Fatal("< should fail: 99 not < 10")
	}
}

// ——————————————————————————————————————————
// EvalExprs — count operator
// ——————————————————————————————————————————

func TestEvalExprs_CountViaCollPath(t *testing.T) {
	payload := []byte(`{"rows":[["a"],["b"],["c"]]}`)
	if err := EvalExprs(payload, []string{"count == 3"}, "rows", nil); err != nil {
		t.Fatalf("count via collPath failed: %v", err)
	}
}

func TestEvalExprs_CountRootArray(t *testing.T) {
	// mongo find / logs: root is an array
	payload := []byte(`[{"id":1},{"id":2}]`)
	if err := EvalExprs(payload, []string{"count == 2"}, "", nil); err != nil {
		t.Fatalf("count root array failed: %v", err)
	}
}

func TestEvalExprs_CountEmpty(t *testing.T) {
	payload := []byte(`[]`)
	if err := EvalExprs(payload, []string{"count == 0"}, "", nil); err != nil {
		t.Fatalf("count empty array == 0 failed: %v", err)
	}
	if err := EvalExprs(payload, []string{"count >= 1"}, "", nil); err == nil {
		t.Fatal("count >= 1 on empty array must fail")
	}
}

// ——————————————————————————————————————————
// EvalExprs — standalone mode (payload = nil)
// ——————————————————————————————————————————

func TestEvalExprs_Standalone_Literals(t *testing.T) {
	if err := EvalExprs(nil, []string{"hello == hello"}, "", nil); err != nil {
		t.Fatalf("standalone == failed: %v", err)
	}
	if err := EvalExprs(nil, []string{"hello != world"}, "", nil); err != nil {
		t.Fatalf("standalone != failed: %v", err)
	}
}

func TestEvalExprs_Standalone_TemplatedVars(t *testing.T) {
	vars := map[string]string{"order_id": "ORD-42", "status": "active"}
	if err := EvalExprs(nil, []string{"{{order_id}} == ORD-42"}, "", vars); err != nil {
		t.Fatalf("standalone templated == failed: %v", err)
	}
	if err := EvalExprs(nil, []string{"{{status}} != closed"}, "", vars); err != nil {
		t.Fatalf("standalone templated != failed: %v", err)
	}
}

func TestEvalExprs_Standalone_MissingVar(t *testing.T) {
	// TEMPLATE_VAR_MISSING propagates
	err := EvalExprs(nil, []string{"{{undefined}} == x"}, "", map[string]string{})
	if err == nil {
		t.Fatal("expected error for undefined template var")
	}
}

// ——————————————————————————————————————————
// EvalExprs — quoting
// ——————————————————————————————————————————

func TestEvalExprs_Quoting_DoubleQuotes(t *testing.T) {
	payload := []byte(`{"msg":"hello world"}`)
	// quoted rhs with spaces
	if err := EvalExprs(payload, []string{`msg == "hello world"`}, "", nil); err != nil {
		t.Fatalf("quoted == failed: %v", err)
	}
}

func TestEvalExprs_Quoting_SingleQuotes(t *testing.T) {
	payload := []byte(`{"msg":"hello world"}`)
	if err := EvalExprs(payload, []string{`msg == 'hello world'`}, "", nil); err != nil {
		t.Fatalf("single-quoted == failed: %v", err)
	}
}

// ——————————————————————————————————————————
// Multiple expressions — short-circuit on first failure
// ——————————————————————————————————————————

func TestEvalExprs_MultipleFirstFailStops(t *testing.T) {
	err := EvalExprs(samplePayload, []string{
		"status_code == 200",
		"status_code == 999", // should fail here
		"body.id == ORD-1",
	}, "", nil)
	if err == nil {
		t.Fatal("expected failure on second expression")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Fatalf("error should mention failing expression, got: %v", err)
	}
}

// ——————————————————————————————————————————
// Extract helper
// ——————————————————————————————————————————

func TestExtract_ExistsAndMissing(t *testing.T) {
	payload := []byte(`{"body":{"id":"ORD-1"}}`)
	val, ok := Extract(payload, "body.id")
	if !ok || val != "ORD-1" {
		t.Fatalf("Extract present: val=%q ok=%v", val, ok)
	}
	_, ok = Extract(payload, "body.nonexistent")
	if ok {
		t.Fatal("Extract missing should return false")
	}
}

func TestParseExpr_OperatorInsideQuotedValue(t *testing.T) {
	// Regression: a contains/equality value containing an operator substring must
	// not be mis-split. The real operator is the leftmost one OUTSIDE quotes.
	cases := []struct{ in, op, lhs, rhs string }{
		{`0.message contains "a == b"`, "contains", "0.message", `"a == b"`},
		{`0.message contains "x >= y"`, "contains", "0.message", `"x >= y"`},
		{`body.note == "a > b"`, "==", "body.note", `"a > b"`},
		{`body.note != "p != q"`, "!=", "body.note", `"p != q"`},
	}
	for _, c := range cases {
		e, err := ParseExpr(c.in)
		if err != nil {
			t.Fatalf("%q: parse error %v", c.in, err)
		}
		if e.Op != c.op || e.LHS != c.lhs || e.RHS != c.rhs {
			t.Fatalf("%q -> {lhs:%q op:%q rhs:%q}, want {lhs:%q op:%q rhs:%q}", c.in, e.LHS, e.Op, e.RHS, c.lhs, c.op, c.rhs)
		}
	}
}

func TestEvalExprs_ContainsValueWithOperator(t *testing.T) {
	payload := []byte(`{"message":"status == ok now"}`)
	if err := EvalExprs(payload, []string{`message contains "status == ok"`}, "", nil); err != nil {
		t.Fatalf("contains with == in value should pass: %v", err)
	}
	if err := EvalExprs(payload, []string{`message contains "status != ok"`}, "", nil); err == nil {
		t.Fatal("contains should fail when the (operator-containing) substring is absent")
	}
}

func TestParseExpr_EscapedQuoteInValue(t *testing.T) {
	// A backslash-escaped quote inside a value must not end the quoted region,
	// so an operator after it (still inside quotes) stays protected.
	e, err := ParseExpr(`m contains "say \" then == done"`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if e.Op != "contains" || e.LHS != "m" || e.RHS != `"say \" then == done"` {
		t.Fatalf("got {lhs:%q op:%q rhs:%q}", e.LHS, e.Op, e.RHS)
	}
}
