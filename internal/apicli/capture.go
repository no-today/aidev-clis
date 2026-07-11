package apicli

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// evalCapture resolves a capture/extract expression against a response:
//
//	body.<gjson>  (bare <x> == body.<x>) | header.<name> | status
//
// Returns "" on miss.
func evalCapture(expr string, r *RawResponse) string {
	switch {
	case expr == "status":
		return strconv.Itoa(r.Status)
	case strings.HasPrefix(expr, "header."):
		v, _ := r.header(strings.TrimPrefix(expr, "header."))
		return v
	default:
		return gjson.GetBytes(r.Body, strings.TrimPrefix(expr, "body.")).String()
	}
}

// bodyExcerpt returns a trimmed, length-capped view of a response body for error
// messages (login failures). If the body is JSON, values of sensitive-named keys
// are redacted first so a token in a login response can never leak into an error
// message — apicli's "the AI never sees the credential" invariant.
func bodyExcerpt(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	var doc any
	if json.Unmarshal(b, &doc) == nil {
		// Non-HTML-escaping so the marker reads as "<redacted>" not "<…".
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(redactSensitive(doc)) == nil {
			s = strings.TrimRight(buf.String(), "\n")
		}
	}
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// sensitiveKey reports whether a JSON key likely holds a secret value.
func sensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, s := range []string{
		"token", "password", "passwd", "secret", "authorization",
		"cookie", "session", "credential", "signature", "apikey", "accesskey",
	} {
		if strings.Contains(lk, s) {
			return true
		}
	}
	return false
}

// redactSensitive walks a decoded JSON value and replaces values of
// sensitive-named keys with "<redacted>".
func redactSensitive(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if sensitiveKey(k) {
				t[k] = "<redacted>"
			} else {
				t[k] = redactSensitive(val)
			}
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = redactSensitive(e)
		}
		return t
	default:
		return v
	}
}

// mergeVars returns a new map combining all inputs; later maps win on a key clash.
func mergeVars(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// mergeCookieStrings joins two "n=v; ..." cookie strings, skipping empties.
func mergeCookieStrings(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "; " + b
	}
}

// parseSetCookies extracts the leading name=value pair from each Set-Cookie line
// (dropping attributes) and joins them as a "n1=v1; n2=v2" Cookie string.
func parseSetCookies(setCookies []string) string {
	var pairs []string
	for _, sc := range setCookies {
		nv := strings.TrimSpace(strings.SplitN(sc, ";", 2)[0])
		if strings.Contains(nv, "=") {
			pairs = append(pairs, nv)
		}
	}
	return strings.Join(pairs, "; ")
}
