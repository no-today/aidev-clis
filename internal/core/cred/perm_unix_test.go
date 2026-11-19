//go:build unix

package cred

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_RejectsTooOpenOnUnix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	dir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "loose")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	if _, err := Read("loose"); err == nil {
		t.Fatal("0644 credential must be rejected on unix")
	}
}
