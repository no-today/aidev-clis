package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWalkJobs_Recursive(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/json" {
			w.Write([]byte(`{"jobs":[
				{"name":"back","url":"http://x/job/back/","jobs":[
					{"name":"team","url":"http://x/job/back/job/team/","jobs":[
						{"name":"svc-a","url":"http://x/job/back/job/team/job/svc-a/"}]}]},
				{"name":"svc-b","url":"http://x/job/svc-b/"}]}`))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	jobs, err := cl.WalkJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, j := range jobs {
		byPath[j.Path] = j.Name
	}
	if len(jobs) != 2 || byPath["back/team/svc-a"] != "svc-a" || byPath["svc-b"] != "svc-b" {
		t.Fatalf("walk: %+v", jobs)
	}
}
