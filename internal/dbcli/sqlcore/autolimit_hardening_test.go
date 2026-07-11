package sqlcore

import "testing"

// With length-preserving stripLiterals, a LIMIT inside a string literal is not
// mistaken for the real top-level LIMIT, and a literal before the LIMIT doesn't
// throw off detection.
func TestApplyAutoLimit_LiteralOffsets(t *testing.T) {
	// explicit LIMIT 50 after a literal is honored verbatim (≤ MaxLimit)
	got, err := ApplyAutoLimit("SELECT 'a long literal here' AS x FROM t LIMIT 50")
	if err != nil || got != "SELECT 'a long literal here' AS x FROM t LIMIT 50" {
		t.Fatalf("explicit LIMIT after literal: got %q err=%v", got, err)
	}

	// "LIMIT 5" inside a string literal is not the top-level limit → append.
	got, err = ApplyAutoLimit("SELECT 'has LIMIT 5 inside' FROM t")
	if err != nil || got != "SELECT 'has LIMIT 5 inside' FROM t LIMIT 100" {
		t.Fatalf("literal LIMIT mishandled: got %q err=%v", got, err)
	}
}
