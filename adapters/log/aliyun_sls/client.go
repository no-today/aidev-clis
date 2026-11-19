package aliyunsls

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// MaxPerRequest is the GetLogs single-call cap on the `line` parameter.
const MaxPerRequest = 100

// incompleteRetryCap bounds retries when x-log-progress stays "Incomplete".
const incompleteRetryCap = 5

// incompleteBackoff is the pause between Incomplete retries. Declared as var so
// tests can override it to avoid sleeping.
var incompleteBackoff = 300 * time.Millisecond

// nowFn is the wall clock used for signing. Tests may override it.
var nowFn = time.Now

// authErrorCodes are SLS error codes that indicate an auth/permission problem.
var authErrorCodes = map[string]struct{}{
	"Unauthorized":       {},
	"SignatureNotMatch":  {},
	"InvalidAccessKeyId": {},
	"AccessKeyDisabled":  {},
}

// Client issues signed GetLogs calls. NOT safe for concurrent use; callers
// invoke sequentially from a single goroutine.
type Client struct {
	endpoint     string // region host, e.g. cn-hangzhou.log.aliyuncs.com
	project      string
	cred         *Credential
	http         *http.Client
	scheme       string // "https" in production; tests set "http"
	hostOverride string // non-empty overrides project.endpoint in URL (tests only)
}

// NewClient builds a Client. endpoint is the bare region host. noProxy makes
// the client dial direct, ignoring HTTP(S)_PROXY env — spares the caller from
// prefixing NO_PROXY=<endpoint-ip> on every invocation behind a corp proxy.
func NewClient(endpoint, project string, cred *Credential, noProxy bool) *Client {
	hc := &http.Client{Timeout: 30 * time.Second}
	if noProxy {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		hc.Transport = tr
	}
	return &Client{
		endpoint: endpoint,
		project:  project,
		cred:     cred,
		http:     hc,
		scheme:   "https",
	}
}

// LogResult is the per-call (or aggregated) result.
type LogResult struct {
	Count    int                      `json:"count"`
	Total    int                      `json:"total"`
	Logs     []map[string]interface{} `json:"logs"`
	Progress string                   `json:"progress,omitempty"`
}

// singleCall issues one GetLogs request, retrying while progress is Incomplete.
// fromTS/toTS are unix seconds. reverse=true means newest first. offset is the
// row offset; line is the page size (<= MaxPerRequest).
func (c *Client) singleCall(ctx context.Context, logstore, query string, fromTS, toTS int64, line, offset int, reverse bool) (*LogResult, error) {
	var last *LogResult
	for attempt := 0; attempt < incompleteRetryCap; attempt++ {
		select {
		case <-ctx.Done():
			return nil, errs.Timeout("SLS_CTX_CANCELLED", ctx.Err().Error())
		default:
		}
		res, err := c.doGetLogs(ctx, logstore, query, fromTS, toTS, line, offset, reverse)
		if err != nil {
			return nil, err
		}
		last = res
		if res.Progress != "Incomplete" {
			return res, nil
		}
		if attempt < incompleteRetryCap-1 {
			select {
			case <-ctx.Done():
				return nil, errs.Timeout("SLS_CTX_CANCELLED", ctx.Err().Error())
			case <-time.After(incompleteBackoff):
			}
		}
	}
	if last != nil {
		last.Progress = "Incomplete"
	}
	return last, nil
}

func (c *Client) doGetLogs(ctx context.Context, logstore, query string, fromTS, toTS int64, line, offset int, reverse bool) (*LogResult, error) {
	q := url.Values{}
	q.Set("type", "log")
	q.Set("from", strconv.FormatInt(fromTS, 10))
	q.Set("to", strconv.FormatInt(toTS, 10))
	q.Set("query", query)
	q.Set("line", strconv.Itoa(line))
	q.Set("offset", strconv.Itoa(offset))
	q.Set("reverse", strconv.FormatBool(reverse))

	host := c.project + "." + c.endpoint
	if c.hostOverride != "" {
		host = c.hostOverride
	}
	u := url.URL{
		Scheme: c.scheme,
		Host:   host,
		Path:   "/logstores/" + logstore,
		// SIGNING GOTCHA: url.Values.Encode() encodes a space as "+". SLS
		// decodes "+" literally (not as space) when rebuilding its canonical
		// resource, so the server-computed signature would differ from ours
		// (which canonicalizes from the DECODED value). Force RFC3986 %20 so
		// both sides decode the same string. QueryEscape already encodes a
		// literal "+" as %2B, so this replacement only ever touches spaces.
		RawQuery: strings.ReplaceAll(q.Encode(), "+", "%20"),
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errs.General("REQUEST_BUILD_FAILED", err.Error())
	}
	req.Header.Set("Host", u.Host)
	signRequest(req, c.cred, nowFn())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errs.Remote("SLS_HTTP_FAILED", err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, c.classifyError(resp.StatusCode, body, true)
	}
	if resp.StatusCode >= 400 {
		return nil, c.classifyError(resp.StatusCode, body, false)
	}

	var logs []map[string]interface{}
	if err := json.Unmarshal(body, &logs); err != nil {
		return nil, errs.Remote("SLS_BAD_JSON",
			fmt.Sprintf("GetLogs body not a JSON array: %s", truncate(string(body), 300)))
	}
	r := &LogResult{Logs: logs, Progress: resp.Header.Get("x-log-progress")}
	if v := resp.Header.Get("x-log-count"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			r.Count = n
		}
	}
	if r.Count == 0 {
		r.Count = len(logs)
	}
	r.Total = r.Count
	return r, nil
}

// classifyError maps an SLS error body to an *errs.Error. forceAuth=true for
// 401/403; otherwise the errorCode decides auth-vs-other.
func (c *Client) classifyError(status int, body []byte, forceAuth bool) error {
	var apiErr struct {
		ErrorCode    string `json:"errorCode"`
		ErrorMessage string `json:"errorMessage"`
	}
	_ = json.Unmarshal(body, &apiErr)
	_, isAuthCode := authErrorCodes[apiErr.ErrorCode]
	if forceAuth || isAuthCode {
		e := errs.Auth("SLS_AUTH_FAILED",
			fmt.Sprintf("SLS rejected the request (HTTP %d, code=%s: %s) — check AK/SK and the RAM policy (needs log:GetLogStoreLogs on this project/logstore)",
				status, apiErr.ErrorCode, apiErr.ErrorMessage))
		e.RemoteStatus = status
		e.RemoteCode = apiErr.ErrorCode
		return e
	}
	e := errs.Remote("SLS_API_ERROR",
		fmt.Sprintf("SLS API error (HTTP %d, code=%s): %s%s", status, apiErr.ErrorCode,
			truncate(apiErr.ErrorMessage, 300), queryHint(apiErr.ErrorMessage)))
	e.RemoteStatus = status
	e.RemoteCode = apiErr.ErrorCode
	return e
}

// queryHint appends an actionable next step for SLS error messages that are
// cryptic or actively misleading (live-verified failure modes). Empty for
// anything unrecognized.
func queryHint(remoteMsg string) string {
	switch {
	// The remote's own advice here ("wrap : with quotation mark") sends the
	// caller down a dead end: quoting does not make an unindexed field queryable.
	case strings.Contains(remoteMsg, "is not config as key value config"):
		return " — hint: this field has no key-value index in the logstore, so it cannot be queried as field:value (quoting it does NOT help); use full-text search (a bare \"term\") or filter on an indexed field"
	case strings.Contains(remoteMsg, "parse search query error"):
		return " — hint: common causes: field:(a or b) is invalid, repeat the field instead (field: a or field: b); __time__ cannot appear in the query, use --from/--to"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
