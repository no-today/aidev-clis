package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleFull(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/j/9/consoleText" {
			w.Write([]byte("line1\nline2\n"))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	txt, err := cl.Console(context.Background(), "j", 9)
	if err != nil || txt != "line1\nline2\n" {
		t.Fatalf("console=%q err=%v", txt, err)
	}
}
