package aidev

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderRaw(t *testing.T) {
	scene := "companyA"
	inv := Inventory{
		Workspace: Workspace{Scene: &scene, Source: "/p/.aidev.yaml"},
		Tools:     []string{"dbcli", "logcli"},
		Targets:   map[string]Capability{"a-uat": {CLIs: []string{"dbcli", "logcli"}}},
		Apps:      []string{"svc-login"},
	}
	var buf bytes.Buffer
	RenderRaw(&buf, inv)
	out := buf.String()
	for _, want := range []string{"scene: companyA", "tools: dbcli, logcli", "a-uat", "apps: svc-login"} {
		if !strings.Contains(out, want) {
			t.Errorf("raw output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestRenderConfigRawUnknownType(t *testing.T) {
	var buf bytes.Buffer
	RenderConfigRaw(&buf, "unexpected")
	if !strings.Contains(buf.String(), "unrenderable result") {
		t.Errorf("want unrenderable result, got %q", buf.String())
	}
}

func TestRenderRawNoScene(t *testing.T) {
	var buf bytes.Buffer
	RenderRaw(&buf, Inventory{Workspace: Workspace{Scene: nil, Source: "none"}, Tools: []string{}})
	if !strings.Contains(buf.String(), "scene: (none)") {
		t.Errorf("want scene: (none), got %q", buf.String())
	}
}
