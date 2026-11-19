package jcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeCred drops a {user,api_token} cred file into a temp AIDEV_CLIS_HOME.
func writeCred(t *testing.T, name, user, token string) {
	t.Helper()
	home := os.Getenv("AIDEV_CLIS_HOME")
	dir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"user": user, "api_token": token})
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClient_AuthAndCrumb(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	var gotAuth, gotCrumb string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumbRequestField": "Jenkins-Crumb", "crumb": "C1"})
		case "/ping":
			gotAuth = r.Header.Get("Authorization")
			gotCrumb = r.Header.Get("Jenkins-Crumb")
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	cl, err := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := cl.do(context.Background(), http.MethodPost, "/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth == "" {
		t.Error("missing Authorization header")
	}
	if gotCrumb != "C1" {
		t.Errorf("crumb not sent on POST: %q", gotCrumb)
	}
}
