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

func TestToCurlRendersFormParts(t *testing.T) {
	tg := &Target{BaseURL: "https://h.example"}
	parts, err := ParseFormArgs([]string{
		"entranceExitId=11085",
		"images=@/tmp/a.png",
		"images=@/tmp/b.png;type=image/png",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ToCurl(tg, &CallRequest{Method: "POST", Path: "/api/upload", Form: parts})
	for _, want := range []string{
		"curl -X POST",
		"-F 'entranceExitId=11085'",
		"-F 'images=@/tmp/a.png'",
		"-F 'images=@/tmp/b.png;type=image/png'",
		"https://h.example/api/upload",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("curl output missing %q in:\n%s", want, got)
		}
	}
	// The boundary is curl's to generate — printing ours would make the
	// command non-reproducible.
	if strings.Contains(got, "boundary") {
		t.Errorf("curl output must not pin a boundary:\n%s", got)
	}
}
