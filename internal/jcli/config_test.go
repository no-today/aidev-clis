package jcli

import "testing"

func TestResolveJobName(t *testing.T) {
	g := &Group{
		JobTemplate:  "${service}-uat",
		JobTemplates: map[string]string{"api-": "${service}-api-uat", "api-core-": "core-${service}"},
		JobOverrides: map[string]string{"legacy": "legacy/build/main"},
	}
	cases := map[string]string{
		"legacy":     "legacy/build/main",
		"api-core-x": "core-api-core-x",
		"api-foo":    "api-foo-api-uat",
		"web-foo":    "web-foo-uat",
	}
	for svc, want := range cases {
		if got := g.ResolveJobName(svc); got != want {
			t.Errorf("ResolveJobName(%q)=%q want %q", svc, got, want)
		}
	}
	if (&Group{}).ResolveJobName("svc") != "svc" {
		t.Error("empty group should return bare service")
	}
}

func TestParseConfig_Groups(t *testing.T) {
	raw := map[string]any{
		"base_url":             "https://j.example.com",
		"credential":           "jenkins.uat",
		"insecure_skip_verify": true,
		"groups": map[string]any{
			"frontend": map[string]any{
				"job_template": "front-end/grp/${service}",
				"deploy":       map[string]any{"steps": []any{[]any{"bash", "-c", "echo fe"}}},
			},
			"backend": map[string]any{"job_template": "app-server/basic/${service}"},
		},
	}
	c, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://j.example.com" || c.Credential != "jenkins.uat" || !c.InsecureSkipVerify {
		t.Fatalf("bad parse: %+v", c)
	}
	fe, err := c.Group("frontend")
	if err != nil || fe.ResolveJobName("x") != "front-end/grp/x" || fe.Deploy == nil {
		t.Fatalf("frontend: %+v err=%v", fe, err)
	}
	if be, err := c.Group("backend"); err != nil || be.ResolveJobName("x") != "app-server/basic/x" {
		t.Fatalf("backend: %+v err=%v", be, err)
	}
}

func TestParseConfig_GroupsRequired(t *testing.T) {
	raw := map[string]any{"base_url": "https://j.example.com", "credential": "x"}
	if _, err := ParseConfig(raw); err == nil {
		t.Fatal("missing groups: must error")
	}
}

func TestConfig_GroupSelection(t *testing.T) {
	// Multiple groups: empty name must error (Phase 1 has no auto-resolution).
	multi := &Config{Groups: map[string]*Group{"a": {}, "b": {}}}
	if _, err := multi.Group(""); err == nil {
		t.Error("empty group name with multiple groups must error")
	}
	if _, err := multi.Group("nope"); err == nil {
		t.Error("unknown group must error")
	}
	if _, err := multi.Group("a"); err != nil {
		t.Errorf("named group must resolve: %v", err)
	}
	// Exactly one group: empty name resolves it (no --group needed).
	one := &Config{Groups: map[string]*Group{"main": {JobTemplate: "t"}}}
	if g, err := one.Group(""); err != nil || g.JobTemplate != "t" {
		t.Errorf("sole group: %+v err=%v", g, err)
	}
}

func TestParseConfig_MissingBaseURL(t *testing.T) {
	if _, err := ParseConfig(map[string]any{"credential": "x", "groups": map[string]any{"m": map[string]any{}}}); err == nil {
		t.Fatal("missing base_url must error")
	}
}

func TestGroupForJob(t *testing.T) {
	cfg := &Config{Groups: map[string]*Group{
		"frontend": {
			JobTemplate:  "front-end/grp/${service}",
			JobOverrides: map[string]string{"ansible-deploy": "front-end/ansible-deploy"},
		},
		"backend": {
			JobTemplate:  "app-server/basic/${service}",
			JobTemplates: map[string]string{"lecshop_": "app-server/independent/${service}"},
		},
		"app": {JobTemplate: "${service}-uat"}, // flat suffix template
	}}

	ok := map[string]string{ // jobPath → expected group
		"front-end/grp/demo-web":           "frontend",
		"front-end/ansible-deploy":         "frontend", // override value
		"app-server/basic/order":           "backend",
		"app-server/independent/lecshop_x": "backend", // longest-prefix template
		"order-uat":                        "app",     // flat suffix reversed
	}
	for path, want := range ok {
		g, err := cfg.GroupForJob(path)
		if err != nil || g != cfg.Groups[want] {
			t.Errorf("GroupForJob(%q) → %v err=%v, want %s", path, g, err, want)
		}
	}

	// Unroutable: lecshop_ under basic/ is never produced by forward routing → no claim.
	if _, err := cfg.GroupForJob("app-server/basic/lecshop_x"); err == nil {
		t.Error("lecshop_ under basic/ must not be claimed")
	}
	// Path under no group at all → JOB_NO_GROUP.
	if _, err := cfg.GroupForJob("random/path/x"); err == nil {
		t.Error("unknown path must error")
	}
}
