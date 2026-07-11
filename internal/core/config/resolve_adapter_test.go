package config

import "testing"

// a multi-adapter config: mysql has 2 targets, kingbase exactly 1, default is a
// kingbase target. (tool key is "logcli" only because writeCfg writes logcli.yaml;
// the adapter values are what ResolveForAdapter keys on.)
const multiAdapter = `
default_target: only_kb
targets:
  uat_my:   {adapter: mysql}
  prd_my:   {adapter: mysql}
  only_kb:  {adapter: kingbase}
`

func TestResolveForAdapter_SingleTargetAutoPicked(t *testing.T) {
	writeCfg(t, multiAdapter)
	// kingbase has exactly one target → used with no --target.
	target, err := ResolveForAdapter("logcli", "", "kingbase")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "only_kb" || target.Adapter != "kingbase" {
		t.Fatalf("got %+v", target)
	}
}

func TestResolveForAdapter_AmbiguousErrors(t *testing.T) {
	writeCfg(t, multiAdapter)
	// mysql has two targets and default_target is kingbase (≠ mysql) → ambiguous.
	_, err := ResolveForAdapter("logcli", "", "mysql")
	if err == nil {
		t.Fatal("expected TARGET_AMBIGUOUS")
	}
}

func TestResolveForAdapter_FlagHonoredVerbatim(t *testing.T) {
	writeCfg(t, multiAdapter)
	// explicit --target is returned as-is (caller checks adapter mismatch).
	target, err := ResolveForAdapter("logcli", "uat_my", "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "uat_my" {
		t.Fatalf("got %+v", target)
	}
}

func TestResolveForAdapter_DefaultTargetUsedWhenItMatches(t *testing.T) {
	writeCfg(t, `
default_target: only_kb
targets:
  only_kb:  {adapter: kingbase}
  a_pg:     {adapter: postgres}
  b_pg:     {adapter: postgres}
`)
	// kingbase has one target (also the default) → picked.
	if target, err := ResolveForAdapter("logcli", "", "kingbase"); err != nil || target.Name != "only_kb" {
		t.Fatalf("kingbase: %+v %v", target, err)
	}
	// postgres has two and is not the default → ambiguous.
	if _, err := ResolveForAdapter("logcli", "", "postgres"); err == nil {
		t.Fatal("postgres: expected ambiguous")
	}
}

func TestResolveForAdapter_NoTargetForAdapter(t *testing.T) {
	writeCfg(t, multiAdapter)
	if _, err := ResolveForAdapter("logcli", "", "redis"); err == nil {
		t.Fatal("expected TARGET_NONE_FOR_ADAPTER")
	}
}

func TestResolveForAdapter_DefaultMatchesAdapterWithMultipleTargets(t *testing.T) {
	// mysql has 2 targets and default_target IS a mysql target → default wins (no error).
	writeCfg(t, `
default_target: prd_my
targets:
  uat_my: {adapter: mysql}
  prd_my: {adapter: mysql}
`)
	target, err := ResolveForAdapter("logcli", "", "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "prd_my" {
		t.Fatalf("got %+v", target)
	}
}
