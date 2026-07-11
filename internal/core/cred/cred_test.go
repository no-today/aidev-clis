package cred

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_OK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "sls.ak"), []byte("secret"), 0o600)
	b, err := Read("sls.ak")
	if err != nil || string(b) != "secret" {
		t.Fatalf("Read: %q %v", b, err)
	}
}

func TestRead_Missing(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	if _, err := Read("nope"); err == nil {
		t.Fatal("want error for missing credential")
	}
}

func TestRead_PathTraversalRejected(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	for _, n := range []string{"../x", "a/b", "..\\x"} {
		if _, err := Read(n); err == nil {
			t.Fatalf("name %q must be rejected", n)
		}
	}
}

func TestPath_OKAndValidated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "kube.cfg"), []byte("apiVersion: v1"), 0o600)
	p, err := Path("kube.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "kube.cfg") {
		t.Fatalf("path=%q", p)
	}
	if _, err := Path("../x"); err == nil {
		t.Fatal("traversal must be rejected by Path too")
	}
}
