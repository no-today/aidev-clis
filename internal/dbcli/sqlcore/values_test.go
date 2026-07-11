package sqlcore

import (
	"strings"
	"testing"
	"time"
)

func TestCoerce(t *testing.T) {
	if got := Coerce([]byte("hi")); got != "hi" {
		t.Errorf("[]byte should become string, got %v", got)
	}
	ts := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	if got := Coerce(ts); got != "2026-06-27T10:00:00Z" {
		t.Errorf("time should be RFC3339, got %v", got)
	}
	if Coerce(nil) != nil {
		t.Error("nil stays nil")
	}
	if Coerce(int64(5)) != int64(5) {
		t.Error("int passes through")
	}
}

func TestTruncateCells(t *testing.T) {
	long := strings.Repeat("x", 300)
	rows := [][]any{{long, "short", int64(7)}}
	cut := TruncateCells(rows, 256)
	if !cut {
		t.Fatal("should report truncation")
	}
	s := rows[0][0].(string)
	if len([]rune(s)) != 257 || !strings.HasSuffix(s, "…") { // 256 + ellipsis
		t.Fatalf("bad truncation: len=%d", len([]rune(s)))
	}
	if rows[0][1] != "short" || rows[0][2] != int64(7) {
		t.Fatal("short string / non-string must be untouched")
	}
}
