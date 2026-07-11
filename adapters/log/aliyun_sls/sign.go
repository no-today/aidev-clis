package aliyunsls

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // SLS API mandates hmac-sha1 for signatureMethod=hmac-sha1; not a free choice
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
	"time"
)

// gmtDate formats t as the RFC1123 GMT date SLS expects, e.g.
// "Mon, 02 Jan 2006 15:04:05 GMT".
func gmtDate(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

// canonicalQuery joins params as sorted "key=value" pairs with the DECODED
// (not percent-encoded) values, matching SLS's CanonicalizedResource rule.
func canonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// canonicalLogHeaders renders x-log-*/x-acs-* headers as sorted lowercase
// "key:value" lines (stringToSign joins them with "\n").
func canonicalLogHeaders(h map[string]string) []string {
	normalized := make(map[string]string, len(h))
	for k, v := range h {
		normalized[strings.ToLower(k)] = v
	}
	keys := make([]string, 0, len(normalized))
	for k := range normalized {
		if strings.HasPrefix(k, "x-log-") || strings.HasPrefix(k, "x-acs-") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+":"+normalized[k])
	}
	return lines
}

// stringToSign assembles the canonical string per Aliyun Log signature v1.
// canonicalizedResource is the path plus optional "?"+canonicalQuery.
func stringToSign(method, contentMD5, contentType, date string, logHeaders map[string]string, canonicalizedResource string) string {
	lines := []string{method, contentMD5, contentType, date}
	lines = append(lines, canonicalLogHeaders(logHeaders)...)
	lines = append(lines, canonicalizedResource)
	return strings.Join(lines, "\n")
}

// signRequest mutates req's headers in place: Date, x-log-apiversion,
// x-log-signaturemethod, optional x-acs-security-token, then Authorization.
// The canonicalizedResource is derived from req.URL.Path plus the request's
// query params (decoded, sorted).
func signRequest(req *http.Request, cred *Credential, now time.Time) {
	date := gmtDate(now)
	req.Header.Set("Date", date)
	req.Header.Set("x-log-apiversion", "0.6.0")
	req.Header.Set("x-log-signaturemethod", "hmac-sha1")
	req.Header.Set("Content-Length", "0")

	logHeaders := map[string]string{
		"x-log-apiversion":      "0.6.0",
		"x-log-signaturemethod": "hmac-sha1",
	}
	if cred.SecurityToken != "" {
		req.Header.Set("x-acs-security-token", cred.SecurityToken)
		logHeaders["x-acs-security-token"] = cred.SecurityToken
	}

	params := map[string]string{}
	for k, vs := range req.URL.Query() {
		if len(vs) > 0 {
			params[k] = vs[0]
		}
	}
	resource := req.URL.Path
	if cq := canonicalQuery(params); cq != "" {
		resource += "?" + cq
	}

	sts := stringToSign(req.Method, "", "", date, logHeaders, resource)
	mac := hmac.New(sha1.New, []byte(cred.AccessKeySecret)) //nolint:gosec // see import comment
	mac.Write([]byte(sts))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("Authorization", "LOG "+cred.AccessKeyID+":"+sig)
}
