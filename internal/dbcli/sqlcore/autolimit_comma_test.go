package sqlcore

import (
	"strings"
	"testing"
)

// MySQL `LIMIT offset, count` — the cap must apply to the COUNT (2nd operand),
// not the offset, so `LIMIT 0,5000` can't sail through (review #1).
func TestApplyAutoLimit_CommaForm(t *testing.T) {
	// count > 100 → error, regardless of offset
	for _, sql := range []string{
		"SELECT * FROM t LIMIT 0,5000",
		"SELECT * FROM t LIMIT 100,101",
		"SELECT * FROM t limit 50 , 9999",
	} {
		if _, err := ApplyAutoLimit(sql); err == nil || !strings.Contains(err.Error(), "LIMIT_TOO_LARGE") {
			t.Errorf("ApplyAutoLimit(%q) should reject (count>100), got %v", sql, err)
		}
	}
	// count ≤ 100 → kept verbatim
	for _, sql := range []string{
		"SELECT * FROM t LIMIT 100,90",
		"SELECT * FROM t LIMIT 0,50",
	} {
		got, err := ApplyAutoLimit(sql)
		if err != nil || got != sql {
			t.Errorf("ApplyAutoLimit(%q) = (%q,%v), want kept", sql, got, err)
		}
	}
}
