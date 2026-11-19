package sqlcore

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"
)

// jsSafeInt is the largest integer a JSON double (IEEE-754) represents exactly.
// AI agents commonly parse JSON with such a number type, so int64 outside this
// range is emitted as a string to avoid silent precision loss.
const jsSafeInt = int64(1) << 53

// Coerce maps a database/sql scan value to a JSON-friendly value.
func Coerce(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(x) {
			return string(x) // string() copies, no aliasing with driver buffers
		}
		return base64.StdEncoding.EncodeToString(x) // binary BLOB → base64, not mojibake
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case int64:
		if x > jsSafeInt || x < -jsSafeInt {
			return strconv.FormatInt(x, 10)
		}
		return x
	case uint64:
		if x > uint64(jsSafeInt) {
			return strconv.FormatUint(x, 10)
		}
		return x
	default:
		// DECIMAL/NUMERIC and other driver types often implement Stringer; render
		// them as their exact string form rather than an opaque struct.
		if s, ok := v.(fmt.Stringer); ok {
			return s.String()
		}
		return v
	}
}

// TruncateCells truncates any string cell longer than max runes to max runes + "…"
// (mutating rows in place). Non-string cells are untouched. Returns whether any
// cell was truncated.
func TruncateCells(rows [][]any, max int) bool {
	cut := false
	for i := range rows {
		for j, cell := range rows[i] {
			s, ok := cell.(string)
			if !ok {
				continue
			}
			r := []rune(s)
			if len(r) > max {
				rows[i][j] = string(r[:max]) + "…"
				cut = true
			}
		}
	}
	return cut
}
