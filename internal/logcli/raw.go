package logcli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// formatRawRecord renders one log record as a single un-enveloped line:
//   - string         -> verbatim (file-tail / console lines)
//   - map with any of time/level/service/trace_id/message -> those parts joined
//   - any other map  -> compact JSON
//   - anything else  -> compact JSON
//
// Best-effort: line adapters wrap lines as {"message": line} so those print
// verbatim; structured records lacking all promoted keys (e.g. Aliyun SLS, whose
// fields are the customer schema + __time__/msg) fall back to compact JSON — a
// faithful, jq-friendly line. We deliberately do not hardcode adapter-specific
// field names here.
func formatRawRecord(rec any) string {
	switch r := rec.(type) {
	case string:
		return r
	case map[string]any:
		if line, ok := promotedLine(r); ok {
			return line
		}
		return compactJSON(r)
	default:
		return compactJSON(rec)
	}
}

func promotedLine(m map[string]any) (string, bool) {
	var parts []string
	add := func(s string) {
		if s != "" {
			parts = append(parts, s)
		}
	}
	add(str(m["time"]))
	add(str(m["level"]))
	if s := str(m["service"]); s != "" {
		parts = append(parts, "["+s+"]")
	}
	if s := str(m["trace_id"]); s != "" {
		parts = append(parts, "trace="+s)
	}
	add(str(m["message"]))
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " "), true
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
