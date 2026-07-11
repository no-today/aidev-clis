package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/no-today/aidev-clis/internal/jcli"
)

func TestCompleteJob_UsesCachedJobPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := "default_target: uat\ntargets:\n  uat:\n    adapter: jenkins\n    base_url: http://uat\n    credential: jenkins.uat\n    groups:\n      main: {}\n  prod:\n    adapter: jenkins\n    base_url: http://prod\n    credential: jenkins.prod\n    groups:\n      main: {}\n"
	if err := os.WriteFile(filepath.Join(home, "jcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&jcli.JobsCache{Target: "uat", Jobs: []jcli.CachedJob{
		{Name: "api", Path: "backend/api"},
		{Name: "web", Path: "frontend/web"},
	}}).Save(); err != nil {
		t.Fatal(err)
	}
	if err := (&jcli.JobsCache{Target: "prod", Jobs: []jcli.CachedJob{
		{Name: "api", Path: "prod/api"},
	}}).Save(); err != nil {
		t.Fatal(err)
	}

	cmd := buildCmd()
	got, _ := completeJob(cmd, nil, "")
	want := []string{"backend/api", "frontend/web", "prod/api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("job completion = %v, want %v", got, want)
	}

	_ = cmd.Flag("target").Value.Set("uat")
	got, _ = completeJob(cmd, nil, "")
	want = []string{"backend/api", "frontend/web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env-scoped job completion = %v, want %v", got, want)
	}
}
