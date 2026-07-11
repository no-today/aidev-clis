package apicli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func init() { maxResponseBytes = 4096 }

const testCapBytes = 4096

func TestDoRequestCapsBodySize(t *testing.T) {
	oversize := testCapBytes + 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(make([]byte, oversize))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "none"}}
	_, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/big"}, Session{})
	if err == nil || !strings.Contains(err.Error(), "RESPONSE_TOO_LARGE") {
		t.Fatalf("expected RESPONSE_TOO_LARGE, got %v", err)
	}
}

func TestDoRequestStreamsToFile(t *testing.T) {
	payload := []byte("PK\x03\x04 fake xlsx bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.ms-excel")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(200)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "x.xlsx")
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "none"}}
	resp, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/export", OutputFile: out}, Session{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		t.Fatalf("file mode must not hold body in memory")
	}
	if resp.BodyFile != out || resp.BodyBytes != int64(len(payload)) || !resp.StreamComplete {
		t.Fatalf("bad metadata: %+v", resp)
	}
	if resp.ContentLength != int64(len(payload)) {
		t.Fatalf("content_length %d != %d", resp.ContentLength, len(payload))
	}
	got, _ := os.ReadFile(out)
	if string(got) != string(payload) {
		t.Fatalf("file content mismatch")
	}
	sum := sha256.Sum256(payload)
	if resp.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 mismatch")
	}
}

func TestDoRequestWritesHeadersFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="r.xlsx"`)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("DATA"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	out := filepath.Join(dir, "r.xlsx")
	hdr := filepath.Join(dir, "r.headers.json")
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "none"}}
	_, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/export", OutputFile: out, HeadersFile: hdr}, Session{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(hdr)
	if err != nil {
		t.Fatalf("headers file not written: %v", err)
	}
	var doc struct {
		StatusCode int               `json:"status_code"`
		URL        string            `json:"url"`
		Headers    map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("headers file not valid JSON: %s", b)
	}
	if doc.StatusCode != 200 {
		t.Fatalf("status_code = %d", doc.StatusCode)
	}
	if doc.Headers["Content-Disposition"] == "" {
		t.Fatalf("expected Content-Disposition in headers file, got: %s", b)
	}
}

func TestDoRequestInjectsSessionAndBody(t *testing.T) {
	var gotAuth, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("X-Test", "1")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	tg := &Target{
		BaseURL: srv.URL,
		Auth:    Auth{Inject: AuthInject{Header: "Authorization: Bearer {{token}}"}},
	}
	req := &CallRequest{
		Method:  "POST",
		Path:    "/api/order",
		Headers: []string{"Content-Type: application/json"},
		Body:    []byte(`{"a":1}`),
	}
	sess := Session{Vars: map[string]string{"token": "T0K"}}
	resp, err := DoRequest(tg, req, sess)
	if err != nil {
		t.Fatalf("DoRequest: %v", err)
	}
	if gotAuth != "Bearer T0K" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotMethod != "POST" || gotBody != `{"a":1}` {
		t.Errorf("method/body = %q %q", gotMethod, gotBody)
	}
	if resp.Status != 201 || resp.Headers["X-Test"] != "1" || string(resp.Body) != `{"code":0}` {
		t.Errorf("resp = %+v", resp)
	}
}

func TestDoRequestAbsoluteURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: "http://ignored.example"}
	// An absolute URL to a host other than the base is cross-origin; this test
	// exercises the "absolute URL overrides base" path, so allow it explicitly.
	resp, err := DoRequest(tg, &CallRequest{Method: "GET", Path: srv.URL + "/x", AllowCrossOrigin: true}, Session{})
	if err != nil || string(resp.Body) != "ok" {
		t.Fatalf("absolute url: %+v %v", resp, err)
	}
}

func TestDoRequestInsecureSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Without skip-verify: the self-signed cert is rejected.
	if _, err := DoRequest(&Target{BaseURL: srv.URL}, &CallRequest{Method: "GET", Path: "/x"}, Session{}); err == nil {
		t.Error("expected TLS verification error without insecure_skip_verify")
	}
	// With skip-verify: succeeds.
	resp, err := DoRequest(&Target{BaseURL: srv.URL, InsecureSkipVerify: true}, &CallRequest{Method: "GET", Path: "/x"}, Session{})
	if err != nil || string(resp.Body) != "ok" {
		t.Fatalf("insecure_skip_verify request: %+v %v", resp, err)
	}
}

func TestDoRequestBlocksCrossOrigin(t *testing.T) {
	tg := &Target{BaseURL: "https://shop.internal", Auth: Auth{Kind: "none"}}
	req := &CallRequest{Method: "GET", Path: "https://evil.example.com/steal"}
	_, err := DoRequest(tg, req, Session{})
	if err == nil || !strings.Contains(err.Error(), "API_CROSS_ORIGIN_URL") {
		t.Fatalf("expected API_CROSS_ORIGIN_URL, got %v", err)
	}
	req.AllowCrossOrigin = true
	// now it must get PAST the guard (it will fail to dial, that's fine)
	_, err = DoRequest(tg, req, Session{})
	if err != nil && strings.Contains(err.Error(), "API_CROSS_ORIGIN_URL") {
		t.Fatalf("guard should be bypassed with AllowCrossOrigin")
	}
}

func TestDoRequestMergesExtraHeaders(t *testing.T) {
	var gotClientID, gotOverride string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID = r.Header.Get("Client-Id")
		gotOverride = r.Header.Get("X-Over")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tg := &Target{
		BaseURL: srv.URL, Auth: Auth{Kind: "none"},
		ExtraHeaders: map[string]string{"Client-Id": "demo_app", "X-Over": "from-config"},
	}
	req := &CallRequest{Method: "GET", Path: "/x", Headers: []string{"X-Over: from-call"}}
	if _, err := DoRequest(tg, req, Session{}); err != nil {
		t.Fatal(err)
	}
	if gotClientID != "demo_app" {
		t.Fatalf("extra_headers not applied: %q", gotClientID)
	}
	if gotOverride != "from-call" {
		t.Fatalf("per-call -H must override extra_headers: %q", gotOverride)
	}
}

func TestDoRequestInjectsHeaderMap(t *testing.T) {
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, k := range []string{"Client-Id", "Authorization", "Access-Token", "X-Call"} {
			got[k] = r.Header.Get(k)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Inject: AuthInject{
		Headers: map[string]string{
			"Client-Id":     "app2",
			"Authorization": "Bearer {{token}}",
			"Access-Token":  "{{accessToken}}",
		},
	}}}
	sess := Session{Vars: map[string]string{"token": "T", "accessToken": "A"}}
	req := &CallRequest{Method: "GET", Path: "/x", Headers: []string{"Authorization: per-call"}}
	if _, err := DoRequest(tg, req, sess); err != nil {
		t.Fatal(err)
	}
	if got["Client-Id"] != "app2" || got["Access-Token"] != "A" {
		t.Fatalf("inject map not applied: %+v", got)
	}
	if got["Authorization"] != "per-call" {
		t.Fatalf("per-call -H must override an injected header, got %q", got["Authorization"])
	}
}

func TestDoRequestInjectsActorVars(t *testing.T) {
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got["Hos-Id"] = r.Header.Get("Hos-Id")
		got["Authorization"] = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	// hosId is a per-account actor var (tg.Vars), NOT captured (sess.Vars).
	// token is in BOTH — the captured value must win.
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Inject: AuthInject{
		Headers: map[string]string{
			"Hos-Id":        "{{hosId}}",
			"Authorization": "Bearer {{token}}",
		},
	}}, Vars: map[string]string{"hosId": "100316", "token": "ACTOR-STALE"}}
	sess := Session{Vars: map[string]string{"token": "CAPTURED"}}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/x"}, sess); err != nil {
		t.Fatal(err)
	}
	if got["Hos-Id"] != "100316" {
		t.Fatalf("inject must render a per-account actor var: got %q", got["Hos-Id"])
	}
	if got["Authorization"] != "Bearer CAPTURED" {
		t.Fatalf("captured var must win over actor var: got %q", got["Authorization"])
	}
}

func TestDoRequestMergesCookieHeaders(t *testing.T) {
	var cookieLines int
	var cookieVal string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieLines = len(r.Header["Cookie"])
		cookieVal = r.Header.Get("Cookie")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tg := &Target{BaseURL: srv.URL, Auth: Auth{Kind: "flow", Inject: AuthInject{Cookie: "A={{a}}"}}}
	sess := Session{Vars: map[string]string{"a": "1"}, Cookie: "JSESSIONID=xyz"}
	if _, err := DoRequest(tg, &CallRequest{Method: "GET", Path: "/x"}, sess); err != nil {
		t.Fatal(err)
	}
	if cookieLines != 1 {
		t.Fatalf("expected exactly ONE Cookie header, got %d (%q)", cookieLines, cookieVal)
	}
	if !strings.Contains(cookieVal, "A=1") || !strings.Contains(cookieVal, "JSESSIONID=xyz") {
		t.Fatalf("merged cookie missing a part: %q", cookieVal)
	}
}

func TestToCurlRedactsHeaderMap(t *testing.T) {
	tg := &Target{BaseURL: "http://h", Auth: Auth{Inject: AuthInject{
		Headers: map[string]string{"Authorization": "Bearer {{token}}"},
	}}}
	out := ToCurl(tg, &CallRequest{Method: "GET", Path: "/x"})
	if strings.Contains(out, "{{token}}") || !strings.Contains(out, "<token>") {
		t.Fatalf("curl must redact injected map header: %s", out)
	}
}

func TestDoRequestJoinsMultiValueHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	resp, err := DoRequest(&Target{BaseURL: srv.URL}, &CallRequest{Method: "GET", Path: "/x"}, Session{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Headers["Set-Cookie"]; got != "a=1, b=2" {
		t.Errorf("Set-Cookie = %q, want \"a=1, b=2\"", got)
	}
}
