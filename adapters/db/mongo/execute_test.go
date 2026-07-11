package mongo

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestFindLimit(t *testing.T) {
	// no .limit() → default cap
	st, _ := ParseStatement(`db.t.find({})`)
	n, err := findLimit(st)
	if err != nil || n != defaultDocCap {
		t.Fatalf("default limit: %d %v", n, err)
	}
	// .limit(50) honored
	st, _ = ParseStatement(`db.t.find({}).limit(50)`)
	if n, _ := findLimit(st); n != 50 {
		t.Fatalf("limit 50: %d", n)
	}
	// .limit(5000) → error
	st, _ = ParseStatement(`db.t.find({}).limit(5000)`)
	if _, err := findLimit(st); err == nil || !strings.Contains(err.Error(), "LIMIT_TOO_LARGE") {
		t.Fatalf("limit 5000 should error, got %v", err)
	}
	// .limit(0) → default cap, NOT 0 (mongo treats SetLimit(0) as "no limit")
	st, _ = ParseStatement(`db.t.find({}).limit(0)`)
	if n, err := findLimit(st); err != nil || n != defaultDocCap {
		t.Fatalf("limit 0 must fall back to default cap: %d %v", n, err)
	}
	// non-numeric .limit("abc") → asInt64 yields 0 → default cap
	st, _ = ParseStatement(`db.t.find({}).limit("abc")`)
	if n, err := findLimit(st); err != nil || n != defaultDocCap {
		t.Fatalf(`limit "abc" must fall back to default cap: %d %v`, n, err)
	}
}

// TestExecuteArity proves methods that index st.Args return a clean
// MONGO_BAD_ARGS error (not a panic) when called with too few arguments.
// The arity guard runs before coll is touched, so a nil collection is safe.
func TestExecuteArity(t *testing.T) {
	cases := []struct {
		name string
		st   *Statement
	}{
		{"insertOne", &Statement{Method: "insertOne"}},
		{"insertMany", &Statement{Method: "insertMany"}},
		{"replaceOne", &Statement{Method: "replaceOne", Args: []any{bson.D{}}}},
		{"findOneAndUpdate", &Statement{Method: "findOneAndUpdate", Args: []any{bson.D{}}}},
		{"findOneAndReplace", &Statement{Method: "findOneAndReplace", Args: []any{bson.D{}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := Execute(context.Background(), nil, c.st)
			if err == nil {
				t.Fatalf("%s: want arity error, got nil", c.name)
			}
			if !strings.Contains(err.Error(), "MONGO_BAD_ARGS") {
				t.Fatalf("%s: want MONGO_BAD_ARGS, got %v", c.name, err)
			}
		})
	}
}

func TestFilterArg(t *testing.T) {
	st, _ := ParseStatement(`db.t.find()`)
	if d := filterArg(st, 0); d == nil {
		t.Fatal("missing filter should default to empty doc, not nil")
	}
}
