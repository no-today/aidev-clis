package jcli

import "testing"

func TestTemplater(t *testing.T) {
	tm := &Templater{
		Service:  "svc",
		Params:   map[string]string{"branch": "main"},
		Vars:     map[string]string{"kc": "/k"},
		Extracts: map[string]string{"tag": "T1"},
	}
	cases := map[string]string{
		"${service}":          "svc",
		"${branch}":           "main",
		"${param.branch}":     "main",
		"${vars.kc}":          "/k",
		"${tag}":              "T1",
		"a-${service}-${tag}": "a-svc-T1",
		"$${HOME}":            "${HOME}",
		"literal {brace}":     "literal {brace}",
		"no tokens":           "no tokens",
	}
	for in, want := range cases {
		got, err := tm.Expand(in)
		if err != nil || got != want {
			t.Errorf("Expand(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := tm.Expand("${nope}"); err == nil {
		t.Error("unknown token must error")
	}
}

func TestTemplater_Regex(t *testing.T) {
	tm := &Templater{Service: "my.api"}
	got, err := tm.ExpandRegex(`reg/${service}:(\d+)`)
	if err != nil || got != `reg/my\.api:(\d+)` {
		t.Fatalf("ExpandRegex=%q,%v", got, err)
	}
}

func TestTemplater_Artifacts(t *testing.T) {
	tm := &Templater{Artifacts: []Artifact{
		{FileName: "a.tar.gz", RelativePath: "sub/a.tar.gz"},
		{FileName: "b.zip", RelativePath: "b.zip"},
	}}
	cases := map[string]string{
		"${artifacts.0}":              "a.tar.gz",
		"${artifacts.0.fileName}":     "a.tar.gz",
		"${artifacts.0.relativePath}": "sub/a.tar.gz",
		"${artifacts.1.fileName}":     "b.zip",
	}
	for in, want := range cases {
		got, err := tm.Expand(in)
		if err != nil || got != want {
			t.Errorf("Expand(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"${artifacts.2}", "${artifacts.0.nope}", "${artifacts.x}"} {
		if _, err := tm.Expand(bad); err == nil {
			t.Errorf("%s must error", bad)
		}
	}
}
