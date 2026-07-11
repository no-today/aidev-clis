package apicli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	cfg, _ := LoadConfig()
	actors, _ := LoadActors()

	// default actor + default env
	tg, err := Resolve(cfg, actors, Selector{App: "svc-login"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tg.BaseURL != "http://uat.example.com" {
		t.Errorf("base = %q", tg.BaseURL)
	}
	if tg.Actor != "alice" || tg.Vars["phoneNo"] != "144" {
		t.Errorf("actor/vars = %q %+v", tg.Actor, tg.Vars)
	}
	if tg.Env != "" { // default env -> empty marker
		t.Errorf("env = %q, want default marker \"\"", tg.Env)
	}

	// named env switches base
	tg, _ = Resolve(cfg, actors, Selector{App: "svc-login", Actor: "bob", Env: "pre"})
	if tg.BaseURL != "http://pre.example.com" || tg.Actor != "bob" {
		t.Errorf("pre/bob = %q %q", tg.BaseURL, tg.Actor)
	}

	// base-url override beats env
	tg, _ = Resolve(cfg, actors, Selector{App: "svc-login", BaseURL: "http://adhoc:9000"})
	if tg.BaseURL != "http://adhoc:9000" {
		t.Errorf("override = %q", tg.BaseURL)
	}
}

func TestResolveErrors(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	cfg, _ := LoadConfig()
	actors, _ := LoadActors()
	cases := []struct {
		name string
		sel  Selector
	}{
		{"unknown app", Selector{App: "ghost"}},
		{"unknown env", Selector{App: "svc-login", Env: "nope"}},
		{"env+baseurl conflict", Selector{App: "svc-login", Env: "pre", BaseURL: "http://x"}},
	}
	for _, c := range cases {
		if _, err := Resolve(cfg, actors, c.sel); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestResolveExpandsSecretRefs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	credDir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "shop_pw"), []byte("s3cr3t"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Apps: map[string]*App{"shop": {BaseURL: "http://x", DefaultActor: "demo"}}}
	actors := &Actors{Actors: map[string]map[string]map[string]string{
		"shop": {"demo": {"password": "secret:shop_pw", "user": "demo@x"}},
	}}
	tg, err := Resolve(cfg, actors, Selector{App: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Vars["password"] != "s3cr3t" {
		t.Fatalf("secret not expanded: %q", tg.Vars["password"])
	}
	if tg.Vars["user"] != "demo@x" {
		t.Fatalf("plain var corrupted: %q", tg.Vars["user"])
	}
}

func TestResolveRejectsBadNames(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	cfg, _ := LoadConfig()
	actors, _ := LoadActors()
	bad := []Selector{
		{App: "svc-login", Actor: "../escape"},
		{App: "svc-login", Env: "../../etc"},
		{App: "svc-login", Actor: "a/b"},
	}
	for _, sel := range bad {
		if _, err := Resolve(cfg, actors, sel); err == nil {
			t.Errorf("expected rejection for %+v", sel)
		}
	}
}
