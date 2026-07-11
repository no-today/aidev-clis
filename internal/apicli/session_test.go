package apicli

import (
	"os"
	"runtime"
	"testing"
)

func TestSessionRoundTrip(t *testing.T) {
	writeHome(t, sampleAPICLI, "")
	tg := &Target{App: "svc-login", Actor: "alice", Env: ""}

	if _, ok := LoadSession(tg); ok {
		t.Fatal("no session should exist yet")
	}
	want := Session{Vars: map[string]string{"token": "abc"}}
	if err := SaveSession(tg, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := LoadSession(tg)
	if !ok || got.Vars["token"] != "abc" {
		t.Fatalf("load = %+v ok=%v", got, ok)
	}
	if runtime.GOOS != "windows" {
		if mode := sessionMode(t, tg); mode != 0o600 {
			t.Errorf("session file mode = %o, want 600", mode)
		}
	}
	if err := DeleteSession(tg); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := LoadSession(tg); ok {
		t.Fatal("session should be gone")
	}
}

func TestSessionPathIsolatesAxes(t *testing.T) {
	a := &Target{App: "svc-login", Actor: "alice", Env: ""}
	b := &Target{App: "svc-login", Actor: "bob", Env: ""}
	c := &Target{App: "svc-login", Actor: "alice", Env: "pre"}
	if sessionPath(a) == sessionPath(b) || sessionPath(a) == sessionPath(c) {
		t.Error("sessions must be isolated per (app, actor, env)")
	}
}

func TestSaveSessionOverwrite(t *testing.T) {
	writeHome(t, sampleAPICLI, "")
	tg := &Target{App: "svc-login", Actor: "alice", Env: ""}
	if err := SaveSession(tg, Session{Vars: map[string]string{"token": "v1"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSession(tg, Session{Vars: map[string]string{"token": "v2"}}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, ok := LoadSession(tg)
	if !ok || got.Vars["token"] != "v2" {
		t.Fatalf("after overwrite got %+v ok=%v", got, ok)
	}
}

func TestSessionAgeSeconds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	tg := &Target{App: "shop", Actor: "demo"}
	if err := SaveSession(tg, Session{Vars: map[string]string{"token": "x"}}); err != nil {
		t.Fatal(err)
	}
	age, ok := SessionAgeSeconds(tg)
	if !ok || age < 0 {
		t.Fatalf("expected a non-negative age, got %d ok=%v", age, ok)
	}
}

func sessionMode(t *testing.T, tg *Target) uint32 {
	t.Helper()
	fi, err := os.Stat(sessionPath(tg))
	if err != nil {
		t.Fatal(err)
	}
	return uint32(fi.Mode().Perm())
}
