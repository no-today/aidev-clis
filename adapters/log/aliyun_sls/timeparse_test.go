package aliyunsls

import "testing"

func TestParseTime(t *testing.T) {
	if _, err := ParseTime("now"); err != nil {
		t.Fatalf("now: %v", err)
	}
	if _, err := ParseTime("5m"); err != nil {
		t.Fatalf("5m: %v", err)
	}
	if _, err := ParseTime("2026-01-02T15:04:05Z"); err != nil {
		t.Fatalf("rfc3339: %v", err)
	}
	if ts, err := ParseTime("1700000000"); err != nil || ts != 1700000000 {
		t.Fatalf("raw unix seconds: ts=%d err=%v", ts, err)
	}
	if _, err := ParseTime("garbage"); err == nil {
		t.Fatal("want error for garbage")
	}
}
