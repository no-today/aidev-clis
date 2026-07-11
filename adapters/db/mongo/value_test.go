package mongo

import (
	"reflect"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestParseValue_Primitives(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{`"hello"`, "hello"},
		{`'single'`, "single"},
		{`42`, int64(42)},
		{`-7`, int64(-7)},
		{`3.14`, 3.14},
		{`1e3`, float64(1000)},
		{`true`, true},
		{`false`, false},
		{`null`, nil},
		{`"with \"escape\" and \n"`, "with \"escape\" and \n"},
	}
	for _, c := range cases {
		got, err := ParseValue(c.in)
		if err != nil {
			t.Errorf("ParseValue(%q) errored: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseValue(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestParseValue_Objects(t *testing.T) {
	// unquoted keys, single/double quotes, $-keys, nesting, trailing comma, whitespace
	got, err := ParseValue(`{ status: "active", age: {$gt: 18}, 'tags': ["a", 'b',], }`)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := got.(bson.D)
	if !ok {
		t.Fatalf("want bson.D, got %T", got)
	}
	if d[0].Key != "status" || d[0].Value != "active" {
		t.Fatalf("first pair: %+v", d[0])
	}
	if d[1].Key != "age" {
		t.Fatalf("second key: %s", d[1].Key)
	}
	age := d[1].Value.(bson.D)
	if age[0].Key != "$gt" || age[0].Value != int64(18) {
		t.Fatalf("nested $gt: %+v", age)
	}
	tags := d[2].Value.(bson.A)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("array: %v", tags)
	}
}

func TestParseValue_Helpers(t *testing.T) {
	oid, _ := ParseValue(`ObjectId("507f1f77bcf86cd799439011")`)
	if _, ok := oid.(primitive.ObjectID); !ok {
		t.Fatalf("ObjectId → %T", oid)
	}
	d, _ := ParseValue(`ISODate("2026-06-27T10:00:00Z")`)
	if _, ok := d.(primitive.DateTime); !ok {
		t.Fatalf("ISODate → %T", d)
	}
	if v, _ := ParseValue(`NumberLong(9007199254740993)`); v != int64(9007199254740993) {
		t.Fatalf("NumberLong → %v", v)
	}
	if v, _ := ParseValue(`NumberInt(5)`); v != int32(5) {
		t.Fatalf("NumberInt → %v", v)
	}
	if _, ok := ParseValueMust(t, `NumberDecimal("12.34")`).(primitive.Decimal128); !ok {
		t.Fatal("NumberDecimal type")
	}
	if _, ok := ParseValueMust(t, `/foo.*/i`).(primitive.Regex); !ok {
		t.Fatal("regex type")
	}
	// new Date(...) and bare MinKey/MaxKey
	if _, ok := ParseValueMust(t, `new Date("2026-01-01T00:00:00Z")`).(primitive.DateTime); !ok {
		t.Fatal("new Date type")
	}
	if _, ok := ParseValueMust(t, `MinKey`).(primitive.MinKey); !ok {
		t.Fatal("MinKey type")
	}
}

func TestParseValue_Malformed_NeverPanics(t *testing.T) {
	for _, bad := range []string{
		`{`, `}`, `{a:}`, `{a 1}`, `[1,`, `"unterminated`, `'`, `{a:1`,
		`ObjectId(`, `ObjectId("nothex")`, `NumberInt(abc)`, `/unterminated`,
		`undefined`, `{,}`, `tru`, `{a:1,,b:2}`, ``, `   `, `{"a":1}garbage`,
	} {
		// must return an error, must not panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseValue(%q) PANICKED: %v", bad, r)
				}
			}()
			if _, err := ParseValue(bad); err == nil {
				t.Errorf("ParseValue(%q) should error", bad)
			} else if !strings.Contains(err.Error(), "MONGO_PARSE") {
				t.Errorf("ParseValue(%q) wrong error code: %v", bad, err)
			}
		}()
	}
}

// ParseValueMust is a test helper.
func ParseValueMust(t *testing.T, in string) any {
	t.Helper()
	v, err := ParseValue(in)
	if err != nil {
		t.Fatalf("ParseValue(%q): %v", in, err)
	}
	return v
}

// Extra coverage beyond the plan: edge cases a real mongosh user would write.
func TestParseValue_Extra(t *testing.T) {
	// empty object
	if v := ParseValueMust(t, `{}`); len(v.(bson.D)) != 0 {
		t.Fatal("empty object should be empty bson.D")
	}
	// empty array
	if v := ParseValueMust(t, `[]`); len(v.(bson.A)) != 0 {
		t.Fatal("empty array should be empty bson.A")
	}
	// date-only string (no time component)
	if _, ok := ParseValueMust(t, `ISODate("2026-01-01")`).(primitive.DateTime); !ok {
		t.Fatal("date-only ISODate should be DateTime")
	}
	// MaxKey
	if _, ok := ParseValueMust(t, `MaxKey`).(primitive.MaxKey); !ok {
		t.Fatal("MaxKey type")
	}
	// $merge pipeline stage detection via AggregateWrites
	pipe, err := ParseValue(`[{$merge: "archive"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if !AggregateWrites(pipe) {
		t.Fatal("$merge pipeline must be a write")
	}
	// deeply nested object
	v, err := ParseValue(`{a:{b:{c:{d:42}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	// walk the nesting
	d := v.(bson.D)[0].Value.(bson.D)[0].Value.(bson.D)[0].Value.(bson.D)[0].Value
	if d != int64(42) {
		t.Fatalf("deeply nested value: %v", d)
	}
	// Timestamp helper
	if _, ok := ParseValueMust(t, `Timestamp(1000, 1)`).(primitive.Timestamp); !ok {
		t.Fatal("Timestamp type")
	}
	// whitespace-only input must error
	if _, err := ParseValue(`   `); err == nil {
		t.Fatal("whitespace-only should error")
	}
	// new Date with date-only string
	if _, ok := ParseValueMust(t, `new Date("2026-06-27T00:00:00Z")`).(primitive.DateTime); !ok {
		t.Fatal("new Date type")
	}
}
