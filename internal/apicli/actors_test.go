package apicli

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleActors = `
actors:
  svc-login:
    alice: { phoneNo: "144", orgId: "999" }
    bob: { phoneNo: "155", orgId: "999" }
`

func TestLoadActors(t *testing.T) {
	writeHome(t, sampleAPICLI, sampleActors)
	a, err := LoadActors()
	if err != nil {
		t.Fatalf("LoadActors: %v", err)
	}
	vars := a.Vars("svc-login", "alice")
	if vars["phoneNo"] != "144" {
		t.Errorf("phoneNo = %q", vars["phoneNo"])
	}
	if a.Vars("svc-login", "ghost") != nil {
		t.Error("unknown actor should return nil")
	}
}

func TestLoadInlineActor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.yaml")
	if err := os.WriteFile(p, []byte("vars:\n  phoneNo: \"139\"\n  verifyCode: \"9999\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vars, err := LoadInlineActor(p)
	if err != nil {
		t.Fatal(err)
	}
	if vars["phoneNo"] != "139" || vars["verifyCode"] != "9999" {
		t.Fatalf("inline actor vars wrong: %+v", vars)
	}
}

func TestLoadActorsMissingIsEmpty(t *testing.T) {
	writeHome(t, sampleAPICLI, "")
	a, err := LoadActors()
	if err != nil {
		t.Fatalf("missing actors.yaml must be OK: %v", err)
	}
	if a.Vars("svc-login", "alice") != nil {
		t.Error("expected nil for empty actors")
	}
}
