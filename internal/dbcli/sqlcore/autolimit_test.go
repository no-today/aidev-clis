package sqlcore

import (
	"strings"
	"testing"
)

func TestApplyAutoLimit(t *testing.T) {
	cases := []struct {
		sql, want string
	}{
		// no LIMIT → append the default
		{"SELECT * FROM t", "SELECT * FROM t LIMIT 100"},
		{"select a from t where b=1", "select a from t where b=1 LIMIT 100"},
		// explicit LIMIT ≤ MaxLimit → kept verbatim (any casing)
		{"SELECT * FROM t LIMIT 5", "SELECT * FROM t LIMIT 5"},
		{"SELECT * FROM t LIMIT 100", "SELECT * FROM t LIMIT 100"},
		{"SELECT * FROM t limit 100", "SELECT * FROM t limit 100"},
		// subquery LIMIT does not count as the top-level limit → append
		{"SELECT * FROM (SELECT x FROM u LIMIT 5) z", "SELECT * FROM (SELECT x FROM u LIMIT 5) z LIMIT 100"},
		// trailing semicolon tolerated
		{"SELECT * FROM t;", "SELECT * FROM t LIMIT 100"},
		// LIMIT with OFFSET (pagination) kept
		{"SELECT * FROM t LIMIT 100 OFFSET 200", "SELECT * FROM t LIMIT 100 OFFSET 200"},
	}
	for _, c := range cases {
		got, err := ApplyAutoLimit(c.sql)
		if err != nil {
			t.Errorf("ApplyAutoLimit(%q) errored: %v", c.sql, err)
			continue
		}
		if got != c.want {
			t.Errorf("ApplyAutoLimit(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}

func TestApplyAutoLimit_TooLargeErrors(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM t LIMIT 101",
		"SELECT * FROM t LIMIT 5000",
		"SELECT * FROM t limit 99999",
	} {
		_, err := ApplyAutoLimit(sql)
		if err == nil || !strings.Contains(err.Error(), "LIMIT_TOO_LARGE") {
			t.Errorf("ApplyAutoLimit(%q) should be LIMIT_TOO_LARGE, got %v", sql, err)
		}
	}
}

func TestApplyAutoLimit_OnlyReads(t *testing.T) {
	got, err := ApplyAutoLimit("SHOW TABLES")
	if err != nil || got != "SHOW TABLES" {
		t.Fatalf("SHOW must be untouched, got %q err=%v", got, err)
	}
}
