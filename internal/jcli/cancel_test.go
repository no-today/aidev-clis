package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCancel(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	var stopped string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crumbIssuer/api/json" {
			w.Write([]byte(`{"crumbRequestField":"X","crumb":"c"}`))
			return
		}
		if r.URL.Path == "/job/j/5/stop" {
			stopped = r.Method
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	if err := cl.Cancel(context.Background(), "j", 5); err != nil {
		t.Fatal(err)
	}
	if stopped != http.MethodPost {
		t.Fatalf("stop not POSTed: %q", stopped)
	}
}
