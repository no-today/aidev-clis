//go:build integration

package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

// TestIT_ThroughBastion connects to mysql via the openssh bastion (password
// auth), proving the full dbcli -> tunnel -> db path. Requires the bastion
// service (make db-up); building it needs build-time network (a proxy in CN —
// see test/integration/bastion/Dockerfile).
func TestIT_ThroughBastion(t *testing.T) {
	if c, err := net.DialTimeout("tcp", "127.0.0.1:2222", time.Second); err != nil {
		t.Skip("bastion not reachable on 127.0.0.1:2222; run make db-up")
	} else {
		_ = c.Close()
	}
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	credDir := filepath.Join(home, "credentials")
	_ = os.MkdirAll(credDir, 0o700)
	_ = os.WriteFile(filepath.Join(credDir, "db.pw"), []byte("ro_pw"), 0o600)
	_ = os.WriteFile(filepath.Join(credDir, "ssh.pw"), []byte("tunnelpw"), 0o600)
	// DSN host is the in-network address reachable FROM the bastion.
	cfg := "default_target: it\ntargets:\n  it:\n" +
		"    adapter: mysql\n" +
		"    dsn: mysql://app_ro@mysql:3306/app\n" +
		"    credential: db.pw\n" +
		"    ssh:\n" +
		"      host: 127.0.0.1\n" +
		"      port: 2222\n" +
		"      user: deploy\n" +
		"      password_credential: ssh.pw\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	code := dbcli.Run(context.Background(), reg, dbcli.RunArgs{
		Driver: "mysql", Target: "it", Args: []string{"SELECT id FROM users WHERE id=1"}, Out: &buf,
	})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope (exit %d): %s", code, buf.String())
	}
	if env["error"] != nil {
		t.Fatalf("query through bastion failed: %s", buf.String())
	}
	rows := env["data"].(map[string]any)["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want 1 row through tunnel, got %s", buf.String())
	}
}
