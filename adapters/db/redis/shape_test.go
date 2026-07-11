package redis

import (
	"reflect"
	"strings"
	"testing"
)

func TestShape_Scalar(t *testing.T) {
	cols, rows := shapeResult("GET", "hello")
	if !reflect.DeepEqual(cols, []string{"value"}) || rows[0][0] != "hello" {
		t.Fatalf("scalar: %v %v", cols, rows)
	}
}

func TestShape_List(t *testing.T) {
	cols, rows := shapeResult("LRANGE", []any{"a", "b"})
	if !reflect.DeepEqual(cols, []string{"index", "value"}) || rows[1][0] != int64(1) || rows[1][1] != "b" {
		t.Fatalf("list: %v %v", cols, rows)
	}
}

func TestShape_HashMap(t *testing.T) {
	cols, rows := shapeResult("HGETALL", map[any]any{"f1": "v1", "f2": "v2"})
	if !reflect.DeepEqual(cols, []string{"field", "value"}) || len(rows) != 2 {
		t.Fatalf("hash: %v %v", cols, rows)
	}
	if rows[0][0] != "f1" { // sorted by field
		t.Fatalf("hash not sorted: %v", rows)
	}
}

func TestNormalize_BinaryAndDuration(t *testing.T) {
	if normalizeValue([]byte{0xff, 0xfe}) != "//4=" { // invalid utf8 → base64
		t.Fatalf("binary: %v", normalizeValue([]byte{0xff, 0xfe}))
	}
	if normalizeValue([]byte("ok")) != "ok" {
		t.Fatalf("utf8 bytes → string")
	}
}

// TestTruncateRows covers review #2: a fat value cell is capped to cellCap runes
// (with "…") so a multi-MB GET/HGET reply can't blow the AI's context.
func TestTruncateRows(t *testing.T) {
	long := strings.Repeat("x", cellCap+10)
	rows := [][]any{{int64(0), long}, {int64(1), "short"}}
	if !truncateRows(rows, cellCap) {
		t.Fatal("truncateRows should report a cut")
	}
	want := strings.Repeat("x", cellCap) + "…"
	if rows[0][1] != want {
		t.Errorf("long cell not capped: len=%d", len([]rune(rows[0][1].(string))))
	}
	if rows[1][1] != "short" {
		t.Errorf("short cell mutated: %v", rows[1][1])
	}
	if rows[0][0] != int64(0) {
		t.Errorf("non-string cell mutated: %v", rows[0][0])
	}
	// nothing over the cap → no cut
	if truncateRows([][]any{{int64(0), "ok"}}, cellCap) {
		t.Error("truncateRows reported a cut on short input")
	}
}

func TestAffected(t *testing.T) {
	if affectedFromResult("DEL", int64(3)) != 3 {
		t.Fatal("DEL count")
	}
	if affectedFromResult("SET", "OK") != 1 {
		t.Fatal("SET OK -> 1")
	}
	if affectedFromResult("FLUSHDB", "OK") != 0 { // semantic-less → 0
		t.Fatal("FLUSHDB -> 0")
	}
}
