package aliyunsls

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCanonicalQuery_SortedDecoded(t *testing.T) {
	got := canonicalQuery(map[string]string{
		"type":    "log",
		"query":   "traceId: abc",
		"from":    "100",
		"to":      "200",
		"line":    "100",
		"offset":  "0",
		"reverse": "true",
	})
	want := "from=100&line=100&offset=0&query=traceId: abc&reverse=true&to=200&type=log"
	if got != want {
		t.Fatalf("canonicalQuery\n got: %q\nwant: %q", got, want)
	}
}

func TestStringToSign_Exact(t *testing.T) {
	got := stringToSign(
		"GET",
		"",
		"",
		"Mon, 02 Jan 2006 15:04:05 GMT",
		map[string]string{
			"x-log-apiversion":      "0.6.0",
			"x-log-signaturemethod": "hmac-sha1",
		},
		"/logstores/ls1?from=100&to=200",
	)
	want := strings.Join([]string{
		"GET",
		"",
		"",
		"Mon, 02 Jan 2006 15:04:05 GMT",
		"x-log-apiversion:0.6.0",
		"x-log-signaturemethod:hmac-sha1",
		"/logstores/ls1?from=100&to=200",
	}, "\n")
	if got != want {
		t.Fatalf("stringToSign\n got: %q\nwant: %q", got, want)
	}
}

func TestSignRequest_WiresHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://p1.cn-hangzhou.log.aliyuncs.com/logstores/ls1?type=log", nil)
	cred := &Credential{AccessKeyID: "AK", AccessKeySecret: "SK", SecurityToken: "TOK"}
	now := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)

	signRequest(req, cred, now)

	if got := req.Header.Get("Date"); got != "Mon, 02 Jan 2006 15:04:05 GMT" {
		t.Fatalf("Date header = %q", got)
	}
	if got := req.Header.Get("x-log-apiversion"); got != "0.6.0" {
		t.Fatalf("apiversion = %q", got)
	}
	if got := req.Header.Get("x-log-signaturemethod"); got != "hmac-sha1" {
		t.Fatalf("sigmethod = %q", got)
	}
	if got := req.Header.Get("x-acs-security-token"); got != "TOK" {
		t.Fatalf("security-token = %q", got)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "LOG AK:") {
		t.Fatalf("Authorization = %q, want prefix 'LOG AK:'", auth)
	}
	if gotSig := strings.TrimPrefix(auth, "LOG AK:"); gotSig != "ifH+S0gPMNTlLrQRtXLCbPhM+Fk=" {
		t.Fatalf("signature = %q, want %q", gotSig, "ifH+S0gPMNTlLrQRtXLCbPhM+Fk=")
	}
	if got := req.Header.Get("Content-Length"); got != "0" {
		t.Fatalf("Content-Length = %q, want %q", got, "0")
	}
}

func TestSignRequest_NoTokenOmitsHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://p1.cn-hangzhou.log.aliyuncs.com/logstores/ls1?type=log", nil)
	signRequest(req, &Credential{AccessKeyID: "AK", AccessKeySecret: "SK"}, time.Now())
	if _, ok := req.Header["X-Acs-Security-Token"]; ok {
		t.Fatal("security-token header should be absent when token empty")
	}
}
