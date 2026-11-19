package mongo

import (
	"math"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestAggregateWrites_Recursive tests C1: $out/$merge nested in sub-pipelines.
func TestAggregateWrites_Recursive(t *testing.T) {
	writes := []string{
		`[{$facet:{cat:[{$out:"o"}]}}]`,
		`[{$unionWith:{coll:"x",pipeline:[{$merge:"d"}]}}]`,
		`[{$lookup:{from:"x",pipeline:[{$out:"y"}],as:"j"}}]`,
	}
	for _, s := range writes {
		v, err := ParseValue(s)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", s, err)
		}
		if !AggregateWrites(v) {
			t.Errorf("AggregateWrites(%q) = false, want true", s)
		}
	}
	read := `[{$match:{a:1}},{$group:{_id:"$a"}}]`
	v, err := ParseValue(read)
	if err != nil {
		t.Fatalf("ParseValue(%q): %v", read, err)
	}
	if AggregateWrites(v) {
		t.Errorf("AggregateWrites(%q) = true, want false (read-only)", read)
	}
}

// TestHasServerJS covers review #4: $where/$function/$accumulator must be
// detected anywhere in a filter/pipeline so the guard can refuse server-side JS.
func TestHasServerJS(t *testing.T) {
	js := []string{
		`{$where:"this.a>1"}`,
		`{a:1,b:{$function:{body:"f",args:[],lang:"js"}}}`,
		`[{$group:{_id:1,t:{$accumulator:{init:"i"}}}}]`,
		`{$and:[{a:1},{$where:"true"}]}`,
		`[{$lookup:{from:"x",pipeline:[{$match:{$where:"1"}}],as:"j"}}]`,
	}
	for _, s := range js {
		v, err := ParseValue(s)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", s, err)
		}
		if !HasServerJS(v) {
			t.Errorf("HasServerJS(%q) = false, want true", s)
		}
	}
	clean := []string{`{a:1,b:{c:2}}`, `[{$match:{x:{$gt:1}}}]`, `{where:"not an op"}`}
	for _, s := range clean {
		v, err := ParseValue(s)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", s, err)
		}
		if HasServerJS(v) {
			t.Errorf("HasServerJS(%q) = true, want false", s)
		}
	}
}

// TestNumberInt_Overflow tests I2: int32 overflow detection.
func TestNumberInt_Overflow(t *testing.T) {
	overflow := []string{`NumberInt(2147483648)`, `NumberInt("9999999999")`}
	for _, s := range overflow {
		if _, err := ParseValue(s); err == nil {
			t.Errorf("ParseValue(%q) should error (int32 overflow)", s)
		}
	}
	v, err := ParseValue(`NumberInt(5)`)
	if err != nil {
		t.Fatalf("ParseValue(NumberInt(5)): %v", err)
	}
	if v != int32(5) {
		t.Errorf("NumberInt(5) = %v (%T), want int32(5)", v, v)
	}
}

// TestRegex_EscapedSlash tests I3: /a\/b/ produces pattern "a/b"; \d preserved.
func TestRegex_EscapedSlash(t *testing.T) {
	v, err := ParseValue(`/a\/b/`)
	if err != nil {
		t.Fatalf(`ParseValue(/a\/b/): %v`, err)
	}
	re, ok := v.(primitive.Regex)
	if !ok {
		t.Fatalf("want Regex, got %T", v)
	}
	if re.Pattern != "a/b" {
		t.Errorf("Pattern = %q, want %q", re.Pattern, "a/b")
	}

	v2, err := ParseValue(`/\d+/i`)
	if err != nil {
		t.Fatalf(`ParseValue(/\d+/i): %v`, err)
	}
	re2, ok := v2.(primitive.Regex)
	if !ok {
		t.Fatalf("want Regex, got %T", v2)
	}
	if re2.Pattern != `\d+` {
		t.Errorf("Pattern = %q, want %q", re2.Pattern, `\d+`)
	}
	if re2.Options != "i" {
		t.Errorf("Options = %q, want %q", re2.Options, "i")
	}
}

// TestUUID_Binary tests I4: UUID stores 16-byte binary subtype 4.
func TestUUID_Binary(t *testing.T) {
	v, err := ParseValue(`UUID("550e8400-e29b-41d4-a716-446655440000")`)
	if err != nil {
		t.Fatalf("ParseValue UUID: %v", err)
	}
	b, ok := v.(primitive.Binary)
	if !ok {
		t.Fatalf("want Binary, got %T", v)
	}
	if b.Subtype != 4 {
		t.Errorf("Subtype = %d, want 4", b.Subtype)
	}
	if len(b.Data) != 16 {
		t.Errorf("len(Data) = %d, want 16", len(b.Data))
	}
	if _, err := ParseValue(`UUID("nothex")`); err == nil {
		t.Error(`UUID("nothex") should error`)
	}
}

// TestHexEscape tests I5: \x41 hex escape in strings.
func TestHexEscape(t *testing.T) {
	v, err := ParseValue(`"\x41"`)
	if err != nil {
		t.Fatalf(`ParseValue("\x41"): %v`, err)
	}
	if v != "A" {
		t.Errorf(`"\x41" = %q, want "A"`, v)
	}
}

// TestNew_AnyWhitespace tests M6: `new` keyword followed by tab/multiple spaces.
func TestNew_AnyWhitespace(t *testing.T) {
	cases := []string{`new   Date("2026-01-01")`, "new\tDate(\"2026-01-01\")"}
	for _, s := range cases {
		v, err := ParseValue(s)
		if err != nil {
			t.Fatalf("ParseValue(%q): %v", s, err)
		}
		if _, ok := v.(primitive.DateTime); !ok {
			t.Errorf("ParseValue(%q) = %T, want DateTime", s, v)
		}
	}
}

// TestBinData tests M7: BinData(subtype, base64).
func TestBinData(t *testing.T) {
	v, err := ParseValue(`BinData(0,"dGVzdA==")`)
	if err != nil {
		t.Fatalf("ParseValue BinData: %v", err)
	}
	b, ok := v.(primitive.Binary)
	if !ok {
		t.Fatalf("want Binary, got %T", v)
	}
	if b.Subtype != 0 {
		t.Errorf("Subtype = %d, want 0", b.Subtype)
	}
	if string(b.Data) != "test" {
		t.Errorf("Data = %q, want %q", b.Data, "test")
	}
}

// TestComments tests M8: // line and /* block */ comments.
func TestComments(t *testing.T) {
	// trailing line comment in a statement
	_, err := ParseStatement(`db.coll.find({}) // trailing`)
	if err != nil {
		t.Fatalf("ParseStatement with // comment: %v", err)
	}
	// inline block comment in an object value
	v, err := ParseValue(`{ /* c */ a: 1 }`)
	if err != nil {
		t.Fatalf("ParseValue with /* */ comment: %v", err)
	}
	d, ok := v.(bson.D)
	if !ok || len(d) != 1 || d[0].Key != "a" {
		t.Errorf("object with block comment: %#v", v)
	}
}

// TestComments_Regex ensures /regex/ inside an object is NOT eaten as a comment.
func TestComments_Regex(t *testing.T) {
	v, err := ParseValue(`{x:/a/}`)
	if err != nil {
		t.Fatalf("ParseValue({x:/a/}): %v", err)
	}
	d, ok := v.(bson.D)
	if !ok || len(d) != 1 || d[0].Key != "x" {
		t.Fatalf("object: %#v", v)
	}
	if _, ok := d[0].Value.(primitive.Regex); !ok {
		t.Errorf("value = %T, want primitive.Regex", d[0].Value)
	}
}

// TestInfinityNaN tests M10: Infinity and NaN as bare identifiers.
func TestInfinityNaN(t *testing.T) {
	v, err := ParseValue(`Infinity`)
	if err != nil {
		t.Fatalf("ParseValue(Infinity): %v", err)
	}
	f, ok := v.(float64)
	if !ok || !math.IsInf(f, 1) {
		t.Errorf("Infinity = %v (%T), want +Inf", v, v)
	}

	v2, err := ParseValue(`NaN`)
	if err != nil {
		t.Fatalf("ParseValue(NaN): %v", err)
	}
	f2, ok := v2.(float64)
	if !ok || !math.IsNaN(f2) {
		t.Errorf("NaN = %v (%T), want NaN", v2, v2)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		method string
		want   Class
	}{
		{"find", ClassRead}, {"findOne", ClassRead}, {"aggregate", ClassRead},
		{"countDocuments", ClassRead}, {"count", ClassRead}, {"distinct", ClassRead},
		{"estimatedDocumentCount", ClassRead},
		{"insertOne", ClassWrite}, {"insertMany", ClassWrite}, {"updateOne", ClassWrite},
		{"updateMany", ClassWrite}, {"replaceOne", ClassWrite}, {"deleteOne", ClassWrite},
		{"deleteMany", ClassWrite}, {"findOneAndUpdate", ClassWrite},
		{"drop", ClassRefused}, {"dropIndex", ClassRefused}, {"createIndex", ClassRefused},
		{"renameCollection", ClassRefused}, {"bulkWrite", ClassRefused}, {"nonsense", ClassRefused},
	}
	for _, c := range cases {
		if got := Classify(c.method); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.method, got, c.want)
		}
	}
}

func TestAggregateWithOutIsWrite(t *testing.T) {
	pipe, _ := ParseValue(`[{$match:{a:1}}, {$out:"copy"}]`)
	if !AggregateWrites(pipe) {
		t.Fatal("$out pipeline must be a write")
	}
	read, _ := ParseValue(`[{$match:{a:1}}, {$group:{_id:"$a"}}]`)
	if AggregateWrites(read) {
		t.Fatal("read pipeline must not be a write")
	}
}
