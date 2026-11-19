package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArtifacts(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/j/9/api/json" {
			w.Write([]byte(`{"artifacts":[{"fileName":"a.tar.gz","relativePath":"sub/a.tar.gz"}]}`))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	arts, err := cl.Artifacts(context.Background(), "j", 9)
	if err != nil || len(arts) != 1 || arts[0].FileName != "a.tar.gz" || arts[0].RelativePath != "sub/a.tar.gz" {
		t.Fatalf("artifacts=%v err=%v", arts, err)
	}
}

func TestArtifacts_None(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/j/9/api/json" {
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	arts, err := cl.Artifacts(context.Background(), "j", 9)
	if err != nil || len(arts) != 0 {
		t.Fatalf("artifacts=%v err=%v", arts, err)
	}
}
