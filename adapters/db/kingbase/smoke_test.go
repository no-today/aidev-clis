//go:build smoke

// Real-instance kingbase smoke. Skipped unless AIDEV_SMOKE_* is set:
//
//	AIDEV_SMOKE_DSN   e.g. postgres://app_ro@host:54321/bizdb
//	AIDEV_SMOKE_CRED  password (written to a temp credstore)
//
// Run: go test -tags smoke ./adapters/db/kingbase/
// There is no public kingbase docker image; the pgwire path is covered against
// docker postgres, the kingbase delta is unit-tested, and this verifies a real
// licensed instance (same approach the old repo used).
package kingbase

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func TestSmoke_Kingbase(t *testing.T) {
	dsn := os.Getenv("AIDEV_SMOKE_DSN")
	if dsn == "" {
		t.Skip("AIDEV_SMOKE_DSN not set; skipping kingbase smoke")
	}
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	_ = os.MkdirAll(filepath.Join(home, "credentials"), 0o700)
	_ = os.WriteFile(filepath.Join(home, "credentials", "kb.pw"), []byte(os.Getenv("AIDEV_SMOKE_CRED")), 0o600)
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: kingbase\n    dsn: " + dsn + "\n    credential: kb.pw\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	code := dbcli.Run(context.Background(), reg, dbcli.RunArgs{Driver: "kingbase", Target: "it", Args: []string{"databases"}, Out: &buf})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope (exit %d): %s", code, buf.String())
	}
	if env["error"] != nil {
		t.Fatalf("kingbase databases failed: %s", buf.String())
	}
}
