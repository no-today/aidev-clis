package apicli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScenarioPlatThreeInjectedHeaders(t *testing.T) {
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Access-Token", "AT-1")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"token":"BT-1"}}`))
			return
		}
		for _, k := range []string{"Client-Id", "Authorization", "Access-Token"} {
			seen[k] = r.Header.Get(k)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow",
		Flow: []FlowStep{{Request: "POST /login", Assert: "status == 200",
			Capture: map[string]string{"token": "body.data.token", "accessToken": "header.Access-Token"}}},
		Inject: AuthInject{Headers: map[string]string{
			"Client-Id":     "app2",
			"Authorization": "Bearer {{token}}",
			"Access-Token":  "{{accessToken}}",
		}}}}
	sess, err := Login(tg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/api/me"}, sess); err != nil {
		t.Fatal(err)
	}
	if seen["Client-Id"] != "app2" || seen["Authorization"] != "Bearer BT-1" || seen["Access-Token"] != "AT-1" {
		t.Fatalf("app2 headers wrong: %+v", seen)
	}
}

func TestScenarioXxlCookieOnly(t *testing.T) {
	var sawCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "app1-1", Path: "/"})
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":200}`))
			return
		}
		sawCookie = r.Header.Get("Cookie")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow",
		Flow: []FlowStep{{Request: "POST /api/login", Assert: "body.code == 200", CookieFromSetCookie: true}}}}
	sess, err := Login(tg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/api/jobs"}, sess); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawCookie, "JSESSIONID=app1-1") {
		t.Fatalf("app1 cookie not replayed: %q", sawCookie)
	}
}

func TestScenarioDoctorThreeStepFlow(t *testing.T) {
	var step3Body string
	headers := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/s1":
			w.Header().Set("X-Nonce", "NON")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/s2":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":0,"ticket":"TKT"}`))
		case "/s3":
			b, _ := io.ReadAll(r.Body)
			step3Body = string(b)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":0,"token":"DTOK"}`))
		default:
			for _, k := range []string{"Authorization", "Hos-Id", "Prd-Code"} {
				headers[k] = r.Header.Get(k)
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow",
		Flow: []FlowStep{
			{Request: "POST /s1", Assert: "body.code == 0", Capture: map[string]string{"nonce": "header.X-Nonce"}},
			{Request: "POST /s2\nContent-Type: application/json\n\n{\"nonce\":\"{{nonce}}\"}",
				Assert: "body.code == 0", Capture: map[string]string{"ticket": "body.ticket"}},
			{Request: "POST /s3\nContent-Type: application/json\n\n{\"nonce\":\"{{nonce}}\",\"ticket\":\"{{ticket}}\"}",
				Assert: "body.code == 0", Capture: map[string]string{"token": "body.token"}},
		},
		Inject: AuthInject{Headers: map[string]string{
			"Authorization": "Bearer {{token}}",
			"Hos-Id":        "H1",
			"Prd-Code":      "P1",
		}}}}
	sess, err := Login(tg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(step3Body, "NON") || !strings.Contains(step3Body, "TKT") {
		t.Fatalf("doctor step3 missing cross-step captures: %q", step3Body)
	}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/api/x"}, sess); err != nil {
		t.Fatal(err)
	}
	if headers["Authorization"] != "Bearer DTOK" || headers["Hos-Id"] != "H1" || headers["Prd-Code"] != "P1" {
		t.Fatalf("doctor inject headers wrong: %+v", headers)
	}
}
