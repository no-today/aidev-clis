package aidev

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSceneEnvWins(t *testing.T) {
	t.Setenv("AIDEV_SCENE", "companyB")
	s := ResolveScene(t.TempDir())
	if s.Name != "companyB" || s.Source != "AIDEV_SCENE" {
		t.Fatalf("got %+v, want {companyB AIDEV_SCENE}", s)
	}
}

func TestResolveSceneWalkUp(t *testing.T) {
	t.Setenv("AIDEV_SCENE", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".aidev.yaml"), []byte("scene: companyA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	s := ResolveScene(sub)
	if s.Name != "companyA" {
		t.Fatalf("got %+v, want name companyA", s)
	}
}

func TestResolveSceneNone(t *testing.T) {
	t.Setenv("AIDEV_SCENE", "")
	s := ResolveScene(t.TempDir())
	if s.Name != "" || s.Source != "none" {
		t.Fatalf("got %+v, want {\"\" none}", s)
	}
}
