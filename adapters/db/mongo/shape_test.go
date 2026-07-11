package mongo

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestCoerce(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	if coerce(oid) != "507f1f77bcf86cd799439011" {
		t.Fatal("ObjectID → hex")
	}
	dt := primitive.NewDateTimeFromTime(time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC))
	if coerce(dt) != "2026-06-27T10:00:00Z" {
		t.Fatalf("DateTime → RFC3339, got %v", coerce(dt))
	}
	big := int64(9007199254740993)
	if coerce(big) != strconv.FormatInt(big, 10) {
		t.Fatal("big int64 → string")
	}
	// nested doc + array coerced recursively, keys preserved
	doc := bson.M{"_id": oid, "tags": bson.A{"a", int64(1)}, "n": bson.D{{Key: "x", Value: dt}}}
	m := coerce(doc).(map[string]any)
	if m["_id"] != "507f1f77bcf86cd799439011" {
		t.Fatalf("nested oid: %v", m)
	}
	if m["tags"].([]any)[0] != "a" {
		t.Fatalf("array: %v", m["tags"])
	}
	if m["n"].(map[string]any)["x"] != "2026-06-27T10:00:00Z" {
		t.Fatalf("nested date: %v", m["n"])
	}
}

// TestCapStrings covers review #2/#3: a fat string field anywhere in a document
// is truncated to cellCap runes (with "…") so it can't blow the AI's context.
func TestCapStrings(t *testing.T) {
	long := strings.Repeat("x", cellCap+50)
	doc := map[string]any{
		"short": "ok",
		"big":   long,
		"nest":  map[string]any{"inner": long},
		"arr":   []any{"a", long},
	}
	out, cut := capStrings(doc, cellCap)
	if !cut {
		t.Fatal("capStrings should report a cut")
	}
	m := out.(map[string]any)
	if m["short"] != "ok" {
		t.Errorf("short string mutated: %v", m["short"])
	}
	want := strings.Repeat("x", cellCap) + "…"
	if m["big"] != want {
		t.Errorf("top-level not capped: len=%d", len([]rune(m["big"].(string))))
	}
	if m["nest"].(map[string]any)["inner"] != want {
		t.Error("nested string not capped")
	}
	if m["arr"].([]any)[1] != want {
		t.Error("array string not capped")
	}
	// nothing to cut → cut=false
	if _, cut := capStrings(map[string]any{"a": "small"}, cellCap); cut {
		t.Error("capStrings reported a cut on short input")
	}
}
