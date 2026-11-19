package jcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func srv0(r *http.Request) string { return "http://" + r.Host }

func TestTriggerAndWait(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crumbIssuer/api/json":
			_ = json.NewEncoder(w).Encode(map[string]string{"crumbRequestField": "X", "crumb": "c"})
		case r.URL.Path == "/job/build-svc/buildWithParameters":
			w.Header().Set("Location", srv0(r)+"/queue/item/7/")
			w.WriteHeader(201)
		case r.URL.Path == "/queue/item/7/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"executable": map[string]any{"number": 42}})
		case r.URL.Path == "/job/build-svc/42/api/json":
			polls++
			building := polls < 2
			result := ""
			if !building {
				result = "SUCCESS"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"building": building, "result": result, "number": 42})
		}
	}))
	defer srv.Close()

	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	cl.pollEvery = 0 // no sleep in tests

	res, err := cl.Trigger(context.Background(), "build-svc", map[string]string{"branch": "main"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Build != 42 {
		t.Fatalf("build number = %d", res.Build)
	}
	done, err := cl.Wait(context.Background(), "build-svc", 42)
	if err != nil {
		t.Fatal(err)
	}
	if done.Result != "SUCCESS" {
		t.Fatalf("result = %q", done.Result)
	}
}
