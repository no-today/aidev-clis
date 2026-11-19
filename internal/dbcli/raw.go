package dbcli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// renderRaw writes a Batch payload as un-enveloped, pipe-friendly text — the
// old project's `plain` semantics:
//   - Result            -> one tab-separated line per row, NO header
//   - map / struct      -> "key=value", one per line, keys sorted
//   - []scalar          -> one value per line
//   - scalar            -> a single %v line
func renderRaw(w io.Writer, payload any) error {
	switch p := payload.(type) {
	case Result:
		return renderRows(w, p.Rows)
	case *Result:
		return renderRows(w, p.Rows)
	}
	// Everything else: normalize via JSON so any concrete payload (verb
	// metadata structs, maps) renders without per-type reflection here.
	var v any
	b, err := json.Marshal(payload)
	if err != nil {
		_, werr := fmt.Fprintln(w, payload)
		return werr
	}
	if err := json.Unmarshal(b, &v); err != nil {
		_, werr := fmt.Fprintln(w, payload)
		return werr
	}
	return renderValue(w, v)
}

func renderRows(w io.Writer, rows [][]any) error {
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = cell(c)
		}
		if _, err := fmt.Fprintln(w, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return nil
}

func renderValue(w io.Writer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(w, "%s=%s\n", k, cell(t[k])); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, e := range t {
			if _, err := fmt.Fprintln(w, cell(e)); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := fmt.Fprintln(w, cell(v))
		return err
	}
}

// cell renders one value: nil -> "". JSON-roundtripped integers up to 2^53 stay
// integer-clean (above that they have already lost precision, like the envelope
// JSON path); nested maps/slices fall back to compact JSON; everything else is %v.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case map[string]any, []any:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		return fmt.Sprintf("%v", t)
	}
}
