package dataease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

// runDriver invokes the dataease driver with AIDEV_CLIS_HOME already set by the
// caller. raw is the env block; args is the dbcli SQL/verb.
func runDriver(t *testing.T, raw map[string]any, args ...string) (*capOut, error) {
	t.Helper()
	in := dbcli.Input{
		Target: config.Target{Name: "local", Adapter: "dataease", Raw: raw},
		Args:   args,
	}
	out := &capOut{}
	err := adapter{}.Run(context.Background(), in, out)
	return out, err
}

func resultRows(t *testing.T, out *capOut) [][]any {
	t.Helper()
	res, ok := out.payload.(dbcli.Result)
	if !ok {
		t.Fatalf("payload is %T, want dbcli.Result", out.payload)
	}
	return res.Rows
}

func TestRun_QueryLoadsSessionAndReturnsResult(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-token", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	out, err := runDriver(t, map[string]any{
		"base_url":       srv.URL,
		"data_source_id": "ds-1",
		"session":        "dataease.local.session",
	}, "select", "1", "as", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultRows(t, out); !reflect.DeepEqual(got, [][]any{{"1"}}) {
		t.Errorf("rows = %v", got)
	}
}

func TestRun_MissingSessionNoCredentialReturnsSessionMissing(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	_, err := runDriver(t, map[string]any{
		"base_url":       "https://example.test/dataease",
		"data_source_id": "ds-1",
		"session":        "dataease.local.session",
	}, "select 1")
	requireCode(t, err, "DATAEASE_SESSION_MISSING")
}

func TestRun_MissingSessionAutoLoginsAndRetries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeLoginCredential(t, home, "dataease.local.login")

	var loginCalls, queryCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls++
			_, _ = w.Write([]byte(`{"success":true,"data":{"token":"jwt-fresh"}}`))
		case "/dataset/table/sqlPreview":
			queryCalls++
			if r.Header.Get("Authorization") != "jwt-fresh" {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runDriver(t, map[string]any{
		"base_url":         srv.URL,
		"data_source_id":   "ds-1",
		"session":          "dataease.local.session",
		"login_credential": "dataease.local.login",
	}, "select 1 as x")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultRows(t, out); !reflect.DeepEqual(got, [][]any{{"1"}}) {
		t.Errorf("rows = %v", got)
	}
	if loginCalls != 1 || queryCalls != 1 {
		t.Errorf("loginCalls=%d queryCalls=%d, want 1/1", loginCalls, queryCalls)
	}
	sess, err := LoadSession("dataease.local.session", srv.URL, filepath.Join(home, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token != "jwt-fresh" {
		t.Errorf("saved token = %q", sess.Token)
	}
}

func TestRun_ExpiredSessionAutoLoginsAndRetriesOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeLoginCredential(t, home, "dataease.local.login")

	var loginCalls, queryCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls++
			_, _ = w.Write([]byte(`{"success":true,"data":{"token":"jwt-fresh"}}`))
		case "/dataset/table/sqlPreview":
			queryCalls++
			switch r.Header.Get("Authorization") {
			case "jwt-stale":
				http.Error(w, `{"success":false,"message":"token expired"}`, http.StatusUnauthorized)
			case "jwt-fresh":
				_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"2"}]}}`))
			default:
				http.Error(w, "unexpected token", http.StatusForbidden)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-stale", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	out, err := runDriver(t, map[string]any{
		"base_url":         srv.URL,
		"data_source_id":   "ds-1",
		"session":          "dataease.local.session",
		"login_credential": "dataease.local.login",
	}, "select 1 as x")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultRows(t, out); !reflect.DeepEqual(got, [][]any{{"2"}}) {
		t.Errorf("rows = %v", got)
	}
	if loginCalls != 1 || queryCalls != 2 {
		t.Errorf("loginCalls=%d queryCalls=%d, want 1/2", loginCalls, queryCalls)
	}
}

func TestRun_AuthRedirectAutoLoginsAndRetriesOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeLoginCredential(t, home, "dataease.local.login")

	var loginCalls, queryCalls int
	var followed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls++
			_, _ = w.Write([]byte(`{"success":true,"data":{"token":"jwt-fresh"}}`))
		case "/dataset/table/sqlPreview":
			queryCalls++
			switch r.Header.Get("Authorization") {
			case "jwt-stale":
				w.Header().Set("Authentication-Status", "invalid")
				http.Redirect(w, r, "/internal-dataease", http.StatusFound)
			case "jwt-fresh":
				_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"3"}]}}`))
			default:
				http.Error(w, "unexpected token", http.StatusForbidden)
			}
		case "/internal-dataease":
			followed = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"wrong"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-stale", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	out, err := runDriver(t, map[string]any{
		"base_url":         srv.URL,
		"data_source_id":   "ds-1",
		"session":          "dataease.local.session",
		"login_credential": "dataease.local.login",
	}, "select 1 as x")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultRows(t, out); !reflect.DeepEqual(got, [][]any{{"3"}}) {
		t.Errorf("rows = %v", got)
	}
	if loginCalls != 1 || queryCalls != 2 {
		t.Errorf("loginCalls=%d queryCalls=%d, want 1/2", loginCalls, queryCalls)
	}
	if followed {
		t.Error("auth redirect must trigger refresh, not be followed")
	}
}

func TestRun_WAFBlockDoesNotAutoLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	writeLoginCredential(t, home, "dataease.local.login")

	var loginCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCalls++
			_, _ = w.Write([]byte(`{"success":true,"data":{"token":"jwt-fresh"}}`))
		case "/dataset/table/sqlPreview":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`您的请求可能存在威胁`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-token", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	_, err := runDriver(t, map[string]any{
		"base_url":         srv.URL,
		"data_source_id":   "ds-1",
		"session":          "dataease.local.session",
		"login_credential": "dataease.local.login",
	}, "select 1 as x")
	requireCode(t, err, "DATAEASE_WAF_BLOCKED")
	if loginCalls != 0 {
		t.Errorf("loginCalls=%d, WAF block must not refresh session", loginCalls)
	}
}

func TestRun_DoctorPingsWithoutBatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"fields":[{"fieldName":"x"}],"data":[{"x":"1"}]}}`))
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-token", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	out, err := runDriver(t, map[string]any{
		"base_url":       srv.URL,
		"data_source_id": "ds-1",
		"session":        "dataease.local.session",
	}, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	if out.wrote {
		t.Error("doctor must not Batch")
	}
}

// TestRun_RejectsNonReadLocally proves the sqlcore read-only gate rejects a
// write/DDL/multi-statement BEFORE any HTTP call: the server fails the test if
// it is ever reached.
func TestRun_RejectsNonReadLocally(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("HTTP call made for a rejected query: %s", r.URL.Path)
	}))
	defer srv.Close()
	if _, err := SaveSession(&Session{Token: "jwt-token", BaseURL: srv.URL}, "dataease.local.session", filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		code string
	}{
		{"cte-delete", []string{"with x as (select id from t) delete from t where id in (select id from x)"}, "WRITE_NOT_ALLOWED"},
		{"multi-statement", []string{"select 1; drop table t"}, "MULTI_STATEMENT"},
		{"update", []string{"update t set x=1"}, "WRITE_NOT_ALLOWED"},
		{"ddl-drop", []string{"drop table t"}, "DDL_REFUSED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runDriver(t, map[string]any{
				"base_url":       srv.URL,
				"data_source_id": "ds-1",
				"session":        "dataease.local.session",
			}, c.args...)
			requireCode(t, err, c.code)
		})
	}
}

func TestRun_EmptySQLReturnsArgRequired(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	_, err := runDriver(t, map[string]any{
		"base_url":       "https://example.test/dataease",
		"data_source_id": "ds-1",
	})
	requireCode(t, err, "ARG_REQUIRED")
}
