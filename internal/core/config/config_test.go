package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "logcli.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_DefaultTarget(t *testing.T) {
	writeCfg(t, `
default_target: app
targets:
  app:
    adapter: local-file
    path: /tmp/x.log
`)
	target, err := Resolve("logcli", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "app" || target.Adapter != "local-file" {
		t.Fatalf("got %+v", target)
	}
	if target.Raw["path"] != "/tmp/x.log" {
		t.Fatalf("path not in raw: %+v", target.Raw)
	}
}

func TestResolve_FlagOverrides(t *testing.T) {
	writeCfg(t, `
default_target: app
targets:
  app: {adapter: local-file, path: /a}
  other: {adapter: local-file, path: /b}
`)
	target, err := Resolve("logcli", "other")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "other" {
		t.Fatalf("flag did not override: %+v", target)
	}
}

func TestResolve_UnknownTarget(t *testing.T) {
	writeCfg(t, "default_target: app\ntargets: {app: {adapter: local-file}}")
	if _, err := Resolve("logcli", "ghost"); err == nil {
		t.Fatal("want error for unknown target")
	}
}

func TestListTargets_SortedWithDescription(t *testing.T) {
	writeCfg(t, `
targets:
  zebra: {adapter: docker, description: last}
  app:   {adapter: local-file, description: 基线}
  bare:  {adapter: kubectl}
`)
	infos, err := ListTargets("logcli")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 3 {
		t.Fatalf("want 3 targets, got %d", len(infos))
	}
	// sorted by name
	if infos[0].Name != "app" || infos[1].Name != "bare" || infos[2].Name != "zebra" {
		t.Fatalf("not sorted: %+v", infos)
	}
	if infos[0].Adapter != "local-file" || infos[0].Description != "基线" {
		t.Fatalf("app fields wrong: %+v", infos[0])
	}
	if infos[1].Description != "" {
		t.Fatalf("bare target should have empty description, got %q", infos[1].Description)
	}
}

func TestListTargets_MissingConfig(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir()) // no logcli.yaml written
	if _, err := ListTargets("logcli"); err == nil {
		t.Fatal("want error when config file is missing")
	}
}

func TestListTargetsScene(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dbcli.yaml"), []byte(`
targets:
  a-uat: { adapter: mysql, scene: companyA, dsn: "mysql://x" }
  local: { adapter: sqlite, dsn: "file:x" }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDEV_CLIS_HOME", dir)

	infos, err := ListTargets("dbcli")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, i := range infos {
		got[i.Name] = i.Scene
	}
	if got["a-uat"] != "companyA" {
		t.Errorf("a-uat scene = %q, want companyA", got["a-uat"])
	}
	if got["local"] != "" {
		t.Errorf("local scene = %q, want empty (global)", got["local"])
	}
}
