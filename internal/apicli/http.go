package apicli

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// maxResponseBytes bounds the in-memory body read. The --output-file streaming
// path (added later) is exempt — it never holds the whole body in RAM.
var maxResponseBytes int64 = 64 << 20

// CallRequest is the parsed `call` input (curl-faithful).
type CallRequest struct {
	Method  string
	Path    string   // relative (joined to base) or absolute URL
	Headers []string // raw "Name: value" lines, repeatable
	Body    []byte
	Timeout time.Duration

	OutputFile  string
	HeadersFile string

	AllowCrossOrigin bool // permit an absolute URL to a host other than the app base
}

// ResolveURL is the single source of truth for the request URL: an absolute path
// (http:// or https://) is used verbatim; anything else is joined to the app base.
// DoRequest, ToCurl, and the audit layer all call this so the logged URL is
// by-construction identical to the one actually sent.
func ResolveURL(tg *Target, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(tg.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// DoRequest builds and sends one HTTP request, injecting the session per the
// app's Auth.Inject rule. It does not classify the response.
func DoRequest(tg *Target, req *CallRequest, sess Session) (*RawResponse, error) {
	url := ResolveURL(tg, req.Path)
	if err := guardCrossOrigin(tg.BaseURL, url, req.AllowCrossOrigin); err != nil {
		return nil, err
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, errs.General("REQUEST_INVALID", err.Error())
	}
	for k, v := range tg.ExtraHeaders {
		hreq.Header.Set(k, v)
	}
	// Inject templates render from actor/input vars AND captured vars, captured
	// winning on a clash. This lets a session header reference a per-account input
	// var (e.g. hosId) that was never captured from a login response — matching the
	// old apicli, whose session headers rendered from "captured + CLI vars".
	injectVars := mergeVars(tg.Vars, sess.Vars)
	if inj := render(tg.Auth.Inject.Header, injectVars); inj != "" {
		if k, v, ok := strings.Cut(inj, ":"); ok {
			hreq.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	for name, tmpl := range tg.Auth.Inject.Headers {
		if v := render(tmpl, injectVars); v != "" {
			hreq.Header.Set(name, v)
		}
	}
	// One Cookie header (RFC 6265): many servers read only the first, so an
	// inject.cookie + a cookie_from_set_cookie session must be merged, not Add'ed twice.
	var cookieParts []string
	if inj := render(tg.Auth.Inject.Cookie, injectVars); inj != "" {
		cookieParts = append(cookieParts, inj)
	}
	if sess.Cookie != "" {
		cookieParts = append(cookieParts, sess.Cookie)
	}
	if len(cookieParts) > 0 {
		hreq.Header.Set("Cookie", strings.Join(cookieParts, "; "))
	}
	for _, h := range req.Headers { // per-call -H LAST so it overrides injected session headers
		if k, v, ok := strings.Cut(h, ":"); ok {
			hreq.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client, err := httpClient(tg, timeout)
	if err != nil {
		return nil, err
	}
	hresp, err := client.Do(hreq)
	if err != nil {
		if strings.Contains(err.Error(), "Client.Timeout") {
			return nil, errs.Timeout("REQUEST_TIMEOUT", err.Error())
		}
		return nil, errs.Remote("REQUEST_FAILED", err.Error())
	}
	defer hresp.Body.Close()
	headers := map[string]string{}
	for k, vs := range hresp.Header {
		headers[k] = strings.Join(vs, ", ")
	}
	if req.OutputFile != "" {
		return streamToFile(hresp, headers, req)
	}
	rb, err := io.ReadAll(io.LimitReader(hresp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errs.Remote("RESPONSE_READ_FAILED", err.Error())
	}
	if int64(len(rb)) > maxResponseBytes {
		return nil, errs.Remote("RESPONSE_TOO_LARGE",
			fmt.Sprintf("response exceeds %d bytes; use --output-file for large/binary downloads", maxResponseBytes))
	}
	return &RawResponse{
		Status: hresp.StatusCode, Headers: headers, Body: rb,
		SetCookies: hresp.Header.Values("Set-Cookie"),
	}, nil
}

// streamToFile writes the response body to req.OutputFile while computing size
// and sha256 in one pass, optionally writes a headers file, and returns
// metadata only (Body stays nil — binary never enters the JSON envelope).
func streamToFile(hresp *http.Response, headers map[string]string, req *CallRequest) (*RawResponse, error) {
	if dir := filepath.Dir(req.OutputFile); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	f, err := os.OpenFile(req.OutputFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errs.General("OUTPUT_FILE_UNWRITABLE", err.Error())
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), hresp.Body)
	closeErr := f.Close()
	if closeErr != nil {
		return nil, errs.General("OUTPUT_FILE_UNWRITABLE", closeErr.Error())
	}
	resp := &RawResponse{
		Status:         hresp.StatusCode,
		Headers:        headers,
		BodyFile:       req.OutputFile,
		BodyBytes:      n,
		ContentLength:  hresp.ContentLength, // -1 if the server didn't declare it
		StreamComplete: copyErr == nil,
		SHA256:         hex.EncodeToString(h.Sum(nil)),
	}
	if req.HeadersFile != "" {
		if err := writeHeadersFile(req.HeadersFile, hresp.StatusCode, hresp.Request.URL.String(), headers); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func writeHeadersFile(path string, status int, url string, headers map[string]string) error {
	doc := map[string]any{"status_code": status, "url": url, "headers": headers}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return errs.General("HEADERS_FILE_UNWRITABLE", err.Error())
	}
	return nil
}

// guardCrossOrigin blocks sending the injected session to a host other than the
// app's configured base host, unless explicitly allowed.
func guardCrossOrigin(base, target string, allow bool) error {
	if allow {
		return nil
	}
	bu, err := neturl.Parse(base)
	if err != nil {
		return errs.Config("CONFIG_INVALID", "app base_url is not a valid URL")
	}
	tu, err := neturl.Parse(target)
	if err != nil {
		return errs.Config("REQUEST_INVALID", "request URL is not valid")
	}
	if tu.Host != "" && !strings.EqualFold(tu.Host, bu.Host) {
		return errs.Config("API_CROSS_ORIGIN_URL",
			fmt.Sprintf("refusing to send session to %q (app host is %q); pass --allow-cross-origin to override", tu.Host, bu.Host))
	}
	return nil
}

// httpClient builds an HTTP client honoring the app's TLS settings. A plain
// client (no custom transport) is used when neither TLS option is set, so http://
// targets are unaffected.
func httpClient(tg *Target, timeout time.Duration) (*http.Client, error) {
	if !tg.InsecureSkipVerify && tg.CACert == "" {
		return &http.Client{Timeout: timeout}, nil
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: tg.InsecureSkipVerify} //nolint:gosec // opt-in for internal apps
	if tg.CACert != "" {
		pem, err := os.ReadFile(tg.CACert)
		if err != nil {
			return nil, errs.Config("CA_CERT_UNREADABLE", err.Error())
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errs.Config("CA_CERT_INVALID", "ca_cert is not valid PEM")
		}
		tlsCfg.RootCAs = pool
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: tlsCfg}}, nil
}

var injectVarRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// redactTemplate turns an inject template like "Authorization: Bearer {{token}}"
// into "Authorization: Bearer <token>" — the header SHAPE without the live value.
func redactTemplate(tpl string) string {
	if tpl == "" {
		return ""
	}
	return injectVarRe.ReplaceAllString(tpl, "<$1>")
}

// ToCurl renders an equivalent curl command for debugging. The injected auth is
// REDACTED to its shape (e.g. "Bearer <token>") — apicli's core invariant is that
// the AI never sees the credential, so --curl must not leak it into the transcript.
func ToCurl(tg *Target, req *CallRequest) string {
	url := ResolveURL(tg, req.Path)
	method := req.Method
	if method == "" {
		method = "GET"
	}
	var b strings.Builder
	b.WriteString("curl -X " + method)
	for _, h := range req.Headers {
		b.WriteString(" \\\n  -H '" + h + "'")
	}
	if inj := redactTemplate(tg.Auth.Inject.Header); inj != "" {
		b.WriteString(" \\\n  -H '" + inj + "'")
	}
	for name, tmpl := range tg.Auth.Inject.Headers {
		b.WriteString(" \\\n  -H '" + name + ": " + redactTemplate(tmpl) + "'")
	}
	if inj := redactTemplate(tg.Auth.Inject.Cookie); inj != "" {
		b.WriteString(" \\\n  -b '" + inj + "'")
	}
	if len(req.Body) > 0 {
		b.WriteString(" \\\n  -d '" + string(req.Body) + "'")
	}
	b.WriteString(" \\\n  '" + url + "'")
	return b.String()
}

// render substitutes {{var}} tokens from vars; returns "" if any token is unfilled.
func render(tpl string, vars map[string]string) string {
	if tpl == "" {
		return ""
	}
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	if strings.Contains(out, "{{") {
		return ""
	}
	return out
}
