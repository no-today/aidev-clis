package aliyunsls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func newTestClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	host := strings.TrimPrefix(srvURL, "http://")
	c := NewClient("example.com", "proj", &Credential{AccessKeyID: "AK", AccessKeySecret: "SK"}, false)
	c.scheme = "http"
	c.hostOverride = host
	return c
}

func writeLogs(w http.ResponseWriter, logs []map[string]interface{}, progress string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-log-count", strconv.Itoa(len(logs)))
	w.Header().Set("x-log-progress", progress)
	_ = json.NewEncoder(w).Encode(logs)
}

func TestSingleCall_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		writeLogs(w, []map[string]interface{}{{"__time__": "1", "msg": "hi"}}, "Complete")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(res.Logs) != 1 || res.Logs[0]["msg"] != "hi" {
		t.Fatalf("bad logs: %+v", res.Logs)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d", res.Count)
	}
}

func TestSingleCall_IncompleteRetriesThenCompletes(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			writeLogs(w, []map[string]interface{}{{"msg": "partial"}}, "Incomplete")
			return
		}
		writeLogs(w, []map[string]interface{}{{"msg": "done"}}, "Complete")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 calls, got %d", calls)
	}
	if res.Logs[0]["msg"] != "done" {
		t.Fatalf("want final complete page, got %+v", res.Logs)
	}
}

func TestSingleCall_403IsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorCode":"SignatureNotMatch","errorMessage":"bad sig"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	if err == nil || !strings.Contains(err.Error(), "SLS_AUTH_FAILED") {
		t.Fatalf("want SLS_AUTH_FAILED, got %v", err)
	}
}

func TestSingleCall_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"ParameterInvalid","errorMessage":"bad from"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	if err == nil || !strings.Contains(err.Error(), "SLS_API_ERROR") {
		t.Fatalf("want SLS_API_ERROR, got %v", err)
	}
}

func TestSingleCall_APIErrorPreservesRemoteStatusAndCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"ParameterInvalid","errorMessage":"localized or changing message"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	e := errs.From(err)
	if e.RemoteStatus != http.StatusBadRequest || e.RemoteCode != "ParameterInvalid" {
		t.Fatalf("remote status/code = %d/%q, want 400/ParameterInvalid", e.RemoteStatus, e.RemoteCode)
	}
}

func TestSingleCall_IncompleteCapExhausted(t *testing.T) {
	old := incompleteBackoff
	incompleteBackoff = time.Millisecond
	defer func() { incompleteBackoff = old }()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeLogs(w, []map[string]interface{}{{"msg": "partial"}}, "Incomplete")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.singleCall(context.Background(), "ls1", "*", 100, 200, 100, 0, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 5 {
		t.Fatalf("want 5 calls (cap), got %d", calls)
	}
	if res.Progress != "Incomplete" {
		t.Fatalf("want Progress=Incomplete, got %q", res.Progress)
	}
	if len(res.Logs) == 0 {
		t.Fatal("want partial logs returned, got none")
	}
}

// TestDoGetLogs_SpaceEncodedAs20 is the %20 signing regression test.
// url.Values.Encode() uses "+" for spaces; SLS decodes "+" literally, causing
// the server to compute a different canonical resource. We must use %20.
func TestDoGetLogs_SpaceEncodedAs20(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		writeLogs(w, nil, "Complete")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _ = c.doGetLogs(context.Background(), "ls1", "level: ERROR", 100, 200, 10, 0, false)
	if strings.Contains(gotRawQuery, "+") {
		t.Fatalf("RawQuery contains '+' (should use %%20 for spaces): %q", gotRawQuery)
	}
	if !strings.Contains(gotRawQuery, "%20") {
		t.Fatalf("RawQuery must encode the space in 'level: ERROR' as %%20: %q", gotRawQuery)
	}
}

// queryHint corrects SLS error messages that would send an agent down a dead
// end (live-verified failure modes); unrecognized messages get no hint.
func TestQueryHint(t *testing.T) {
	cases := []struct {
		msg      string
		wantHint string // substring; "" = no hint at all
	}{
		{`key (log.level) is not config as key value config,if symbol : is  in your log,please wrap : with quotation mark "`,
			"no key-value index"},
		{`parse search query error,please read query syntax document,error detail:syntax error, unexpected OR`,
			"repeat the field"},
		{`bad from`, ""},
	}
	for _, c := range cases {
		got := queryHint(c.msg)
		if c.wantHint == "" {
			if got != "" {
				t.Errorf("queryHint(%q) = %q, want none", c.msg, got)
			}
			continue
		}
		if !strings.Contains(got, c.wantHint) {
			t.Errorf("queryHint(%q) = %q, want substring %q", c.msg, got, c.wantHint)
		}
	}
}

func TestSingleCall_UnindexedFieldErrorCarriesHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"ParameterInvalid","errorMessage":"key (log.level) is not config as key value config,if symbol : is  in your log,please wrap : with quotation mark \""}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.singleCall(context.Background(), "ls1", "log.level: ERROR", 100, 200, 100, 0, true)
	if err == nil || !strings.Contains(err.Error(), "hint: this field has no key-value index") {
		t.Fatalf("unindexed-field error must carry the corrective hint, got: %v", err)
	}
}

// no_proxy=true must produce a direct transport (Proxy nil) so HTTP(S)_PROXY
// env never intercepts SLS calls for this target.
func TestNewClient_NoProxyBypassesEnvProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1") // unroutable; would break any proxied call
	c := NewClient("example.com", "proj", &Credential{AccessKeyID: "AK", AccessKeySecret: "SK"}, true)
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("noProxy client must have a transport with nil Proxy, got %T", c.http.Transport)
	}
	// default client keeps environment proxying
	d := NewClient("example.com", "proj", &Credential{AccessKeyID: "AK", AccessKeySecret: "SK"}, false)
	if d.http.Transport != nil {
		t.Fatalf("default client should use DefaultTransport (env proxy honored)")
	}
}
