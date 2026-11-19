package jcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParams_Static(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	writeCred(t, "jenkins.t", "ci", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/j/api/json" {
			w.Write([]byte(`{"property":[{"parameterDefinitions":[
				{"name":"branch","type":"StringParameterDefinition","defaultParameterValue":{"value":"master"}},
				{"name":"area","type":"ChoiceParameterDefinition","choices":["uat","prod"],"defaultParameterValue":{"value":"uat"}}]}]}`))
		}
	}))
	defer srv.Close()
	cl, _ := NewClient(&Config{BaseURL: srv.URL, Credential: "jenkins.t"})
	ps, err := cl.Params(context.Background(), "j")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("params: %+v", ps)
	}
	m := map[string]ParamDef{}
	for _, p := range ps {
		m[p.Name] = p
	}
	if m["branch"].Type != "string" || m["branch"].Default != "master" {
		t.Fatalf("branch: %+v", m["branch"])
	}
	if m["area"].Type != "choice" || len(m["area"].Choices) != 2 {
		t.Fatalf("area: %+v", m["area"])
	}
}
