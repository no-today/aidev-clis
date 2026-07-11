package dataease

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestClientLogin_PostsEncryptedCredentialAndCapturesToken(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if v := r.Header.Get("LINK-PWD-TOKEN"); v != "null" {
			t.Errorf("LINK-PWD-TOKEN = %q", v)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"success":true,"message":null,"data":{"token":"jwt-from-body"}}`))
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL, Timeout: defaultTimeout})
	sess, err := client.Login(context.Background(), LoginCredential{Username: "encrypted-user", Password: "encrypted-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/auth/login" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["username"] != "encrypted-user" || gotBody["password"] != "encrypted-pass" || gotBody["loginType"] != float64(0) {
		t.Errorf("body = %v", gotBody)
	}
	if sess.Token != "jwt-from-body" {
		t.Errorf("token = %q", sess.Token)
	}
	if sess.BaseURL != srv.URL {
		t.Errorf("base_url = %q", sess.BaseURL)
	}
}

func TestClientLogin_UsesAuthorizationHeaderFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Authorization", "jwt-from-header")
		_, _ = w.Write([]byte(`{"success":true,"message":null,"data":{}}`))
	}))
	defer srv.Close()

	sess, err := NewClient(&Config{BaseURL: srv.URL, Timeout: defaultTimeout}).
		Login(context.Background(), LoginCredential{Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token != "jwt-from-header" {
		t.Errorf("token = %q", sess.Token)
	}
}

func TestClientLogin_ReturnsAuthErrorWhenNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":null,"data":{}}`))
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, Timeout: defaultTimeout}).
		Login(context.Background(), LoginCredential{Username: "u", Password: "p"})
	requireCode(t, err, "DATAEASE_LOGIN_NO_TOKEN")
}

func TestClientRequestsIncludeBrowserCompatibilityHeaders(t *testing.T) {
	var loginHeaders, queryHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hsytpay/dataease/api/auth/login":
			loginHeaders = r.Header.Clone()
			_, _ = w.Write([]byte(`{"success":true,"message":null,"data":{"token":"jwt-from-login"}}`))
		case "/hsytpay/dataease/dataset/table/sqlPreview":
			queryHeaders = r.Header.Clone()
			_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	baseURL := srv.URL + "/hsytpay/dataease"
	client := NewClient(&Config{BaseURL: baseURL, DataSourceID: "ds-1", Timeout: defaultTimeout})
	sess, err := client.Login(context.Background(), LoginCredential{Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Query(context.Background(), sess, "select 1"); err != nil {
		t.Fatal(err)
	}

	assertBrowserHeaders(t, loginHeaders, srv.URL, baseURL+"/login")
	if c := loginHeaders.Get("Cookie"); !strings.Contains(c, "request-time-out=100") || !strings.Contains(c, "language=zh_CN") {
		t.Errorf("login Cookie = %q", c)
	}
	assertBrowserHeaders(t, queryHeaders, srv.URL, baseURL+"/login")
	if c := queryHeaders.Get("Cookie"); !strings.HasPrefix(c, "request-time-out=100; language=zh_CN; Authorization=") || !strings.Contains(c, "Authorization=jwt-from-login") {
		t.Errorf("query Cookie = %q", c)
	}
}

func TestClientQuery_PostsBase64SQLPreviewPayload(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout})
	if _, err := client.Query(context.Background(), &Session{Token: "jwt"}, "select 1 as x"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/dataset/table/sqlPreview" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["dataSourceId"] != "ds-1" || gotBody["type"] != "sql" || gotBody["mode"] != float64(0) || gotBody["sqlVariableDetails"] != "[]" {
		t.Errorf("body = %v", gotBody)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(gotBody["info"].(string)), &info); err != nil {
		t.Fatal(err)
	}
	if info["sql"] != base64.StdEncoding.EncodeToString([]byte("select 1 as x")) {
		t.Errorf("info.sql = %v", info["sql"])
	}
	if info["isBase64Encryption"] != true {
		t.Errorf("isBase64Encryption = %v", info["isBase64Encryption"])
	}
}

func TestClientQuery_ReplaysTokenInHeaderAndCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "jwt-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if c := r.Header.Get("Cookie"); !strings.Contains(c, "Authorization=jwt-token") {
			t.Errorf("Cookie = %q", c)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt-token"}, "select 1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientQuery_AuthRedirectReturnsAuthExpiredWithoutFollowing(t *testing.T) {
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed = true
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[],"data":[]}}`))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Authentication-Status", "invalid")
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt-stale"}, "select 1")
	requireCode(t, err, "DATAEASE_AUTH_EXPIRED")
	if followed {
		t.Error("auth redirect must not be followed")
	}
}

func TestClientQuery_BareRedirectReturnsAuthExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// gyfe-style bounce: a plain 302 to the login page, no auth headers.
		http.Redirect(w, r, "https://login.example/login", http.StatusFound)
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt-stale"}, "select 1")
	requireCode(t, err, "DATAEASE_AUTH_EXPIRED")
}

func TestClientQuery_WAFBlockReturnsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`您的请求可能存在威胁`))
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt-token"}, "select 1")
	requireCode(t, err, "DATAEASE_WAF_BLOCKED")
}

func TestClientQuery_DataEaseFailureReturnsRemoteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"message":"sql denied"}`))
	}))
	defer srv.Close()

	_, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt-token"}, "select 1")
	requireCode(t, err, "DATAEASE_QUERY_FAILED")
}

func TestClientQuery_Returns200Rows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
	}))
	defer srv.Close()

	res, err := NewClient(&Config{BaseURL: srv.URL, DataSourceID: "ds-1", Timeout: defaultTimeout}).
		Query(context.Background(), &Session{Token: "jwt"}, "select 1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Columns, []string{"x"}) || !reflect.DeepEqual(res.Rows, [][]any{{"1"}}) {
		t.Errorf("result = %+v", res)
	}
}

func assertBrowserHeaders(t *testing.T, h http.Header, origin, referer string) {
	t.Helper()
	if h == nil {
		t.Fatal("headers nil")
	}
	checks := map[string]string{
		"Origin":             origin,
		"Referer":            referer,
		"User-Agent":         dataEaseBrowserUserAgent,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-origin",
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"macOS"`,
	}
	for k, want := range checks {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(h.Get("sec-ch-ua"), "Chromium") {
		t.Errorf("sec-ch-ua = %q", h.Get("sec-ch-ua"))
	}
}
