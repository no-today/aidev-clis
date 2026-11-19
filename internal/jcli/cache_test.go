package jcli

import (
	"path/filepath"
	"testing"
)

func TestJobsCache_RoundTrip(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	in := &JobsCache{
		Target: "uat", BaseURL: "https://j", SyncedAt: "2026-06-28T10:00:00Z",
		Jobs: []CachedJob{
			{Name: "svc-a", Path: "back/team/svc-a", URL: "https://j/job/back/job/team/job/svc-a/"},
			{Name: "svc-b", Path: "svc-b", URL: "https://j/job/svc-b/"},
		},
	}
	if err := in.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJobsCache("uat")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Jobs) != 2 || got.Jobs[0].Path != "back/team/svc-a" || got.BaseURL != "https://j" {
		t.Fatalf("round-trip: %+v", got)
	}
}

func TestLoadJobsCache_Missing(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	if _, err := LoadJobsCache("nope"); err == nil {
		t.Fatal("missing cache must error")
	}
	_ = filepath.Join
}

func TestJobsCache_HasPath(t *testing.T) {
	c := &JobsCache{Jobs: []CachedJob{
		{Name: "svc", Path: "front-end/grp/svc"},
		{Name: "other", Path: "app-server/basic/other"},
	}}
	if !c.HasPath("front-end/grp/svc") {
		t.Error("HasPath should find an existing job path")
	}
	if c.HasPath("app-server/basic/svc") {
		t.Error("HasPath should not find a missing job path")
	}
}

func TestConfig_AutoResolveGroup(t *testing.T) {
	cfg := &Config{Groups: map[string]*Group{
		"frontend": {JobTemplate: "front-end/grp/${service}"},
		"backend":  {JobTemplate: "app-server/basic/${service}"},
	}}
	cache := &JobsCache{Jobs: []CachedJob{
		{Name: "svc", Path: "front-end/grp/svc"},
		{Name: "other", Path: "app-server/basic/other"},
		{Name: "dup", Path: "front-end/grp/dup"},
		{Name: "dup", Path: "app-server/basic/dup"},
	}}

	// Unique claimant → that group + the candidate path.
	grp, job, err := cfg.AutoResolveGroup("svc", cache)
	if err != nil || job != "front-end/grp/svc" || grp != cfg.Groups["frontend"] {
		t.Fatalf("svc → %v %q err=%v", grp, job, err)
	}
	if _, j, err := cfg.AutoResolveGroup("other", cache); err != nil || j != "app-server/basic/other" {
		t.Fatalf("other → %q err=%v", j, err)
	}

	// No claimant → SERVICE_UNKNOWN.
	if _, _, err := cfg.AutoResolveGroup("ghost", cache); err == nil {
		t.Error("unknown service must error")
	}

	// Two claimants → GROUP_AMBIGUOUS.
	if _, _, err := cfg.AutoResolveGroup("dup", cache); err == nil {
		t.Error("ambiguous service must error")
	}

	// Nil cache → GROUP_REQUIRED.
	if _, _, err := cfg.AutoResolveGroup("svc", nil); err == nil {
		t.Error("nil cache must error")
	}
}
