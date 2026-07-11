package apicli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestLoginCrossStepCapture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/step1":
			w.Header().Set("X-Nonce", "N-9")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/step2":
			b, _ := io.ReadAll(r.Body)
			if !bytes.Contains(b, []byte("N-9")) {
				t.Errorf("step2 body missing step1 nonce: %s", b)
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"token":"T-2"}`))
		}
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Flow: []FlowStep{
		{Request: "POST /step1", Capture: map[string]string{"nonce": "header.X-Nonce"}},
		{Request: "POST /step2\nContent-Type: application/json\n\n{\"nonce\":\"{{nonce}}\"}",
			Capture: map[string]string{"token": "body.token"}},
	}}}
	sess, err := Login(tg)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Vars["token"] != "T-2" || sess.Vars["nonce"] != "N-9" {
		t.Fatalf("captures wrong: %+v", sess.Vars)
	}
}

// A wrong password typically returns 200 with an error body, so the token
// capture yields "". Login must reject this with AUTH_FAILED rather than
// returning (and later persisting) a dead session.
func TestLoginFailsOnEmptyCapture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1,"msg":"bad password"}`)) // no token in body
	}))
	defer srv.Close()

	tg := &Target{
		BaseURL: srv.URL,
		Vars:    map[string]string{"phoneNo": "144", "password": "wrong"},
		Auth: Auth{
			Kind: "flow", VarsRequired: []string{"phoneNo", "password"},
			Flow: []FlowStep{{
				Request: "POST /auth/login\nContent-Type: application/json\n\n{\"phoneNo\":\"{{phoneNo}}\"}",
				Capture: map[string]string{"token": "body.data.token"},
			}},
			Inject: AuthInject{Header: "Authorization: Bearer {{token}}"},
		},
	}

	if _, err := Login(tg); err == nil {
		t.Fatal("expected AUTH_FAILED for empty capture")
	} else if e, ok := err.(*errs.Error); !ok || e.Code != "AUTH_FAILED" {
		t.Fatalf("want AUTH_FAILED, got %v", err)
	}
}

func TestLoginSucceedsWhenCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"token":"good"}}`))
	}))
	defer srv.Close()

	tg := &Target{
		BaseURL: srv.URL,
		Vars:    map[string]string{"phoneNo": "144", "password": "right"},
		Auth: Auth{
			Kind: "flow", VarsRequired: []string{"phoneNo", "password"},
			Flow: []FlowStep{{
				Request: "POST /auth/login\nContent-Type: application/json\n\n{\"phoneNo\":\"{{phoneNo}}\"}",
				Capture: map[string]string{"token": "body.data.token"},
			}},
			Inject: AuthInject{Header: "Authorization: Bearer {{token}}"},
		},
	}

	sess, err := Login(tg)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.Vars["token"] != "good" {
		t.Errorf("token = %q, want good", sess.Vars["token"])
	}
}

func TestLoginStepAssertFailsLoudly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":40101,"msg":"bad password"}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Flow: []FlowStep{
		{Request: "POST /login", Assert: "body.code == 0", Capture: map[string]string{"token": "body.token"}},
	}}}
	_, err := Login(tg)
	if err == nil || !strings.Contains(err.Error(), "AUTH_FAILED") {
		t.Fatalf("expected AUTH_FAILED on failed assert, got %v", err)
	}
	if !strings.Contains(err.Error(), "bad password") {
		t.Fatalf("assert failure should include a body excerpt: %v", err)
	}
}

func TestLoginStepAssertErrorNamesTheStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":1}`))
	}))
	defer srv.Close()
	// With a name → the error names it. Without a name → falls back to "step N".
	named := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Flow: []FlowStep{
		{Name: "verify-login", Request: "POST /x", Assert: "body.code == 0"},
	}}}
	_, err := Login(named)
	if err == nil || !strings.Contains(err.Error(), `"verify-login"`) {
		t.Fatalf("named step should appear in the assert error, got %v", err)
	}
	anon := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Flow: []FlowStep{
		{Request: "POST /x", Assert: "body.code == 0"},
	}}}
	_, err = Login(anon)
	if err == nil || !strings.Contains(err.Error(), "step 1") {
		t.Fatalf("anonymous step should fall back to 'step 1', got %v", err)
	}
}

func TestLoginCookieFromSetCookieReplays(t *testing.T) {
	var sawCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "abc", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "z9"})
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			sawCookie = r.Header.Get("Cookie")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Flow: []FlowStep{
		{Request: "POST /login", CookieFromSetCookie: true},
	}}}
	sess, err := Login(tg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.Cookie, "SESSION=abc") || !strings.Contains(sess.Cookie, "csrf=z9") {
		t.Fatalf("session cookie not captured: %q", sess.Cookie)
	}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/data"}, sess); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawCookie, "SESSION=abc") {
		t.Fatalf("cookie not replayed on call: %q", sawCookie)
	}
}

func TestLoginVarsDefaultsPrecedence(t *testing.T) {
	var sawCode, sawUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if bytes.Contains(b, []byte(`"code":"9999"`)) {
			sawCode = "9999"
		}
		if bytes.Contains(b, []byte(`"user":"alice"`)) {
			sawUser = "alice"
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"token":"T"}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Vars: map[string]string{"user": "alice"},
		Auth: Auth{Kind: "flow",
			VarsDefaults: map[string]string{"code": "9999", "user": "DEFAULT"},
			VarsRequired: []string{"code", "user"},
			Flow: []FlowStep{{
				Request: "POST /login\nContent-Type: application/json\n\n{\"code\":\"{{code}}\",\"user\":\"{{user}}\"}",
				Capture: map[string]string{"token": "body.token"},
			}}}}
	if _, err := Login(tg); err != nil {
		t.Fatal(err)
	}
	if sawCode != "9999" {
		t.Fatalf("vars_defaults code not used")
	}
	if sawUser != "alice" {
		t.Fatalf("actor var must override vars_defaults (got DEFAULT)")
	}
}
