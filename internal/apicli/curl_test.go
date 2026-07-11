package apicli

import (
	"strings"
	"testing"
)

func TestToCurl(t *testing.T) {
	tg := &Target{BaseURL: "https://h.example", Auth: Auth{Inject: AuthInject{Header: "Authorization: Bearer {{token}}"}}}
	req := &CallRequest{Method: "POST", Path: "/api/x", Headers: []string{"Content-Type: application/json"}, Body: []byte(`{"a":1}`)}
	got := ToCurl(tg, req)
	// The auth header SHAPE is shown, but the live token value is redacted.
	for _, want := range []string{"curl -X POST", "https://h.example/api/x", "Authorization: Bearer <token>", `-d '{"a":1}'`, "Content-Type: application/json"} {
		if !strings.Contains(got, want) {
			t.Errorf("curl output missing %q in:\n%s", want, got)
		}
	}
}

func TestToCurlRedactsLiveSecrets(t *testing.T) {
	tg := &Target{BaseURL: "https://h.example", Auth: Auth{Inject: AuthInject{
		Header: "Authorization: Bearer {{token}}",
		Cookie: "SESSION={{sid}}",
	}}}
	got := ToCurl(tg, &CallRequest{Method: "GET", Path: "/x"})
	for _, leaked := range []string{"T0K", "SECRET", "abc123"} {
		if strings.Contains(got, leaked) {
			t.Errorf("curl must never contain a live secret %q:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "<token>") || !strings.Contains(got, "<sid>") {
		t.Errorf("expected redacted placeholders for header and cookie:\n%s", got)
	}
}
