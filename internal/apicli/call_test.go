package apicli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestExtractTrace(t *testing.T) {
	r := &RawResponse{
		Status:  200,
		Headers: map[string]string{"X-Trace-Id": "hdr-123"},
		Body:    []byte(`{"data":{"traceId":"body-456"}}`),
	}
	if got := extractTrace("header.X-Trace-Id", r); got != "hdr-123" {
		t.Fatalf("header trace: %q", got)
	}
	if got := extractTrace("body.data.traceId", r); got != "body-456" {
		t.Fatalf("body trace: %q", got)
	}
	if got := extractTrace("data.traceId", r); got != "body-456" {
		t.Fatalf("bare (=body.) trace: %q", got)
	}
	if got := extractTrace("", r); got != "" {
		t.Fatalf("empty field must yield empty")
	}
}

func TestCallSurfacesMalformedPredicate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "none"}, Response: Response{OKWhen: "garbage-no-operator"}}
	if _, err := Call(tg, &CallRequest{Method: "GET", Path: "/x"}); err == nil || !strings.Contains(err.Error(), "PREDICATE_INVALID") {
		t.Fatalf("malformed ok_when should surface PREDICATE_INVALID, got %v", err)
	}
}

// app whose token expires once, forcing exactly one relogin.
func TestCallAutoRelogin(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	var logins int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			atomic.AddInt32(&logins, 1)
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"fresh"}}`))
		default:
			if r.Header.Get("Authorization") == "Bearer fresh" {
				_, _ = w.Write([]byte(`{"code":0,"data":"ok"}`))
			} else {
				_, _ = w.Write([]byte(`{"code":401}`)) // expired_when
			}
		}
	}))
	defer srv.Close()

	tg := &Target{
		App: "svc-login", Actor: "alice", BaseURL: srv.URL,
		Vars: map[string]string{"phoneNo": "144", "password": "p"},
		Auth: Auth{
			Kind: "flow", VarsRequired: []string{"phoneNo", "password"},
			Flow: []FlowStep{{
				Request: "POST /auth/login\nContent-Type: application/json\n\n{\"phoneNo\":\"{{phoneNo}}\",\"password\":\"{{password}}\"}",
				Capture: map[string]string{"token": "body.data.token"},
			}},
			Inject: AuthInject{Header: "Authorization: Bearer {{token}}"},
		},
		Response: Response{OKWhen: "body.code == 0", ExpiredWhen: "body.code == 401"},
	}
	// pre-seed a stale session so the first call hits expired_when
	_ = SaveSession(tg, Session{Vars: map[string]string{"token": "stale"}})

	res, err := Call(tg, &CallRequest{Method: "GET", Path: "/api/me"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.OK {
		t.Errorf("expected ok after relogin; got %+v", res)
	}
	if res.Relogged != true {
		t.Errorf("expected Relogged=true")
	}
	if logins != 1 {
		t.Errorf("expected exactly 1 relogin, got %d", logins)
	}
}

func TestCallExpiredInteractiveGivesUp(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir()) // isolate: ensure no stored session
	tg := &Target{
		App: "interactive", Actor: "x", BaseURL: "http://unused",
		Auth:     Auth{Kind: "flow", VarsRequired: []string{"otp"}}, // otp not in Vars
		Response: Response{ExpiredWhen: "status == 401"},
		Vars:     map[string]string{}, // missing otp -> cannot auto-relogin
	}
	_, err := Call(tg, &CallRequest{Method: "GET", Path: "/x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if e, ok := err.(*errs.Error); !ok || e.Code != "SESSION_EXPIRED" {
		t.Fatalf("want SESSION_EXPIRED, got %v", err)
	}
}

func TestCallNoAuthSkipsLogin(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	tg := &Target{
		App: "svc-open", Actor: "default", BaseURL: srv.URL,
		Auth:     Auth{Kind: "none"},
		Response: Response{OKWhen: "body.code == 0"},
	}
	res, err := Call(tg, &CallRequest{Method: "GET", Path: "/api/x"})
	if err != nil {
		t.Fatalf("no-auth call: %v", err)
	}
	if !res.OK || res.Relogged {
		t.Errorf("want ok=true relogged=false, got %+v", res)
	}
}

func TestCallColdStartExpiredNoDoubleLogin(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	var logins int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" {
			atomic.AddInt32(&logins, 1)
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"fresh"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":401}`)) // always looks expired
	}))
	defer srv.Close()

	tg := &Target{
		App: "svc-login", Actor: "alice", BaseURL: srv.URL,
		Vars: map[string]string{"phoneNo": "144", "password": "p"},
		Auth: Auth{
			Kind: "flow", VarsRequired: []string{"phoneNo", "password"},
			Flow: []FlowStep{{
				Request: "POST /auth/login\nContent-Type: application/json\n\n{\"phoneNo\":\"{{phoneNo}}\",\"password\":\"{{password}}\"}",
				Capture: map[string]string{"token": "body.data.token"},
			}},
			Inject: AuthInject{Header: "Authorization: Bearer {{token}}"},
		},
		Response: Response{OKWhen: "body.code == 0", ExpiredWhen: "body.code == 401"},
	}
	// no pre-seeded session -> cold start

	res, err := Call(tg, &CallRequest{Method: "GET", Path: "/api/me"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if logins != 1 {
		t.Errorf("expected exactly 1 login on cold start, got %d", logins)
	}
	if res.OK {
		t.Errorf("expected OK=false (server always 401), got %+v", res)
	}
	if res.Relogged {
		t.Errorf("expected Relogged=false on cold start, got true")
	}
}
