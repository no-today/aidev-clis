package apicli

import "testing"

func TestPredicateEval(t *testing.T) {
	resp := &RawResponse{
		Status:  200,
		Headers: map[string]string{"X-Auth-Status": "expired"},
		Body:    []byte(`{"code":0,"data":{"token":"t"}}`),
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"body.code == 0", true},
		{"body.code == 401", false},
		{"status == 200", true},
		{"status != 200", false},
		{"header.X-Auth-Status == 'expired'", true},
		{"body.data.token != null", true},
		{"body.missing == null", true},
		{"status == 401 || body.code == 0", true},
		{"status == 401 || body.code == 9", false},
		{"body.code == null", false},    // present field is not null
		{"body.missing != null", false}, // absent field fails != null
		{"body.msg != 'a!=b'", true},    // RHS containing != must parse (msg absent -> != 'a!=b' true)
		{"status == 200 || body.code == 1", true},
	}
	for _, c := range cases {
		got, err := EvalPredicate(c.expr, resp)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got != c.want {
			t.Errorf("%q = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestPredicateHeaderCaseInsensitive(t *testing.T) {
	r := &RawResponse{Status: 200, Headers: map[string]string{"X-Auth": "expired"}}
	got, err := EvalPredicate("header.x-auth == 'expired'", r)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatalf("header predicate should match case-insensitively")
	}
}

func TestPredicateQuotedOperators(t *testing.T) {
	// A quoted rhs may contain "||", "==" or "!=" — these must be treated as
	// opaque value characters, not as a clause separator or operator.
	r := &RawResponse{
		Status: 200,
		Body:   []byte(`{"msg":"a||b","note":"x==y"}`),
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"body.msg == 'a||b'", true},                     // "||" inside quotes is not a separator
		{"body.msg == 'a||c'", false},                    // value differs
		{"body.note == 'x==y'", true},                    // "==" inside quotes is not the operator
		{"body.note != 'x==y'", false},                   // same, negated
		{"body.msg != 'z' || body.note == 'x==y'", true}, // real "||" still splits
	}
	for _, c := range cases {
		got, err := EvalPredicate(c.expr, r)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got != c.want {
			t.Errorf("%q = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestPredicateEmptyIsFalse(t *testing.T) {
	got, err := EvalPredicate("", &RawResponse{Status: 200})
	if err != nil || got {
		t.Errorf("empty predicate must be false,nil; got %v,%v", got, err)
	}
}
