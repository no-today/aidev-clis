package aidev

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func hasApp(apps []string, name string) bool {
	for _, a := range apps {
		if a == name {
			return true
		}
	}
	return false
}

// writeHome lays down a four-tool fixture config and points AIDEV_CLIS_HOME at
// it. Shared by inventory_test.go and run_test.go (same package).
func writeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("dbcli.yaml", `
targets:
  a-uat: { adapter: mysql, scene: companyA, dsn: "mysql://x" }
`)
	must("jcli.yaml", `
targets:
  a-uat: { adapter: jenkins, scene: companyA, base_url: "http://j" }
`)
	must("logcli.yaml", `
targets:
  a-uat:  { adapter: kubectl, scene: companyA }
  a-prod: { adapter: kubectl, scene: companyA }
`)
	must("apicli.yaml", `
apps:
  svc-login: { base_url: "http://x", scene: companyA, auth: { kind: none } }
  svc-other: { base_url: "http://y", scene: companyB, auth: { kind: none } }
`)
	t.Setenv("AIDEV_CLIS_HOME", dir)
	t.Setenv("AIDEV_SCENE", "") // tests pass Scene explicitly
	return dir
}

func TestBuildScopedToScene(t *testing.T) {
	writeHome(t)
	inv, err := Build(Scene{Name: "companyA", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Targets["a-uat"].CLIs; !reflect.DeepEqual(got, []string{"dbcli", "jcli", "logcli"}) {
		t.Errorf("a-uat clis = %v, want [dbcli jcli logcli]", got)
	}
	if got := inv.Targets["a-prod"].CLIs; !reflect.DeepEqual(got, []string{"logcli"}) {
		t.Errorf("a-prod clis = %v, want [logcli]", got)
	}
	if !hasApp(inv.Apps, "svc-login") {
		t.Error("svc-login should be in scope")
	}
	if hasApp(inv.Apps, "svc-other") {
		t.Error("svc-other (companyB) should be out of scene")
	}
	if !reflect.DeepEqual(inv.Tools, []string{"apicli", "dbcli", "jcli", "logcli"}) {
		t.Errorf("tools = %v, want [apicli dbcli jcli logcli]", inv.Tools)
	}
	if inv.Workspace.Scene == nil || *inv.Workspace.Scene != "companyA" {
		t.Errorf("workspace scene = %v, want companyA", inv.Workspace.Scene)
	}
}

func TestBuildNoSceneAllVisible(t *testing.T) {
	writeHome(t)
	inv, err := Build(Scene{Name: "", Source: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasApp(inv.Apps, "svc-other") {
		t.Error("svc-other should be visible when no scene is active")
	}
	if inv.Workspace.Scene != nil {
		t.Errorf("workspace scene = %v, want nil", inv.Workspace.Scene)
	}
}

func TestBuildMissingToolsAreSilent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logcli.yaml"), []byte("targets:\n  x: { adapter: kubectl }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDEV_CLIS_HOME", dir)
	t.Setenv("AIDEV_SCENE", "")
	inv, err := Build(Scene{Name: "", Source: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Targets["x"].CLIs; !reflect.DeepEqual(got, []string{"logcli"}) {
		t.Errorf("x clis = %v, want [logcli]", got)
	}
	if len(inv.Notes) != 0 {
		t.Errorf("missing tool configs must be silent, got notes %v", inv.Notes)
	}
}
