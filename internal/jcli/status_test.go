package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveBuildAndStatus(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/j/api/json":
			w.Write([]byte(`{"lastBuild":{"number":12}}`))
		case "/job/j/12/api/json":
			w.Write([]byte(`{"building":true,"result":""}`))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	res, err := cl.Status(context.Background(), "j", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Build != 12 || !res.Building {
		t.Fatalf("status=%+v", res)
	}
}
