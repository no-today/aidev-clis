package sqlcore

import (
	"strings"
	"testing"
)

func TestApplyAutoLimit_FetchFirst(t *testing.T) {
	// FETCH FIRST/NEXT n ROWS is recognised as the top-level limit (review I1):
	// ≤ MaxLimit kept, no clause appends the default.
	keep := []string{
		"SELECT * FROM t FETCH FIRST 100 ROWS ONLY",
		"SELECT * FROM t FETCH NEXT 50 ROWS ONLY",
		"SELECT * FROM t fetch first 90 rows only",
	}
	for _, sql := range keep {
		got, err := ApplyAutoLimit(sql)
		if err != nil || got != sql {
			t.Errorf("ApplyAutoLimit(%q) = (%q,%v), want kept", sql, got, err)
		}
	}
	// > MaxLimit → error
	if _, err := ApplyAutoLimit("SELECT * FROM t FETCH FIRST 5000 ROWS ONLY"); err == nil || !strings.Contains(err.Error(), "LIMIT_TOO_LARGE") {
		t.Errorf("FETCH FIRST 5000 should be LIMIT_TOO_LARGE, got %v", err)
	}
	// no clause → append
	if got, _ := ApplyAutoLimit("SELECT * FROM t"); got != "SELECT * FROM t LIMIT 100" {
		t.Errorf("no clause should append LIMIT 100, got %q", got)
	}
}
