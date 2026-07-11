//go:build integration

// Integration tests for the mysql driver against the docker DB (make db-up).
// Run: make db-up && go test -tags=integration ./adapters/db/mysql/
package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

// writeEnv points AIDEV_CLIS_HOME at a temp dir with a dbcli.yaml + credential,
// using the given mysql user.
func writeEnv(t *testing.T, user, pass string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "credentials"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "credentials", "db.pw"), []byte(pass), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: mysql\n    dsn: mysql://" + user + "@127.0.0.1:13306/app\n    credential: db.pw\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, allowWrite bool, args ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	code := dbcli.Run(context.Background(), reg, dbcli.RunArgs{
		Driver: "mysql", Target: "it", AllowWrite: allowWrite, Args: args, Out: &buf,
	})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope (exit %d): %s", code, buf.String())
	}
	return env
}

func TestIT_ReadAutoLimitAndCoerce(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "SELECT id, name, big, avatar FROM users ORDER BY id")
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data: %v", env)
	}
	rows := data["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	first := rows[0].([]any)
	if _, isStr := first[2].(string); !isStr {
		t.Fatalf("big int should stringify, got %T", first[2])
	}
	if _, isStr := first[3].(string); !isStr {
		t.Fatalf("binary avatar should be a base64 string, got %T", first[3])
	}
}

func TestIT_WriteDeniedByGuardAndByGrant(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "DELETE FROM users WHERE id=99")
	if env["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatalf("want WRITE_NOT_ALLOWED, got %v", env)
	}
	env = run(t, true, "DELETE FROM users WHERE id=99")
	if env["error"] == nil {
		t.Fatalf("read-only account must reject the write at the DB: %v", env)
	}
}

func TestIT_WriteWithRWAccount(t *testing.T) {
	writeEnv(t, "app_rw", "rw_pw")
	env := run(t, true, "UPDATE orders SET status='PAID' WHERE id=2")
	data := env["data"].(map[string]any)
	if data["affected"].(float64) != 1 {
		t.Fatalf("affected: %v", env)
	}
}

func TestIT_DDLAlwaysRefused(t *testing.T) {
	writeEnv(t, "app_rw", "rw_pw")
	env := run(t, true, "DROP TABLE orders")
	if env["error"].(map[string]any)["code"] != "DDL_REFUSED" {
		t.Fatalf("want DDL_REFUSED, got %v", env)
	}
}

func TestIT_Discovery(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "databases")
	rows := env["data"].(map[string]any)["rows"].([]any)
	if len(rows) < 1 {
		t.Fatalf("databases empty: %v", env)
	}
	env = run(t, false, "describe", "users")
	cols := env["data"].(map[string]any)["columns"].([]any)
	if len(cols) == 0 {
		t.Fatalf("describe users returned no columns: %v", env)
	}
}

func TestIT_TimeoutKill(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	code := dbcli.Run(ctx, reg, dbcli.RunArgs{
		Driver: "mysql", Target: "it", Args: []string{"SELECT SLEEP(5)"}, Out: &buf,
	})
	if code == 0 {
		t.Fatalf("a 5s sleep under a 0.5s timeout must fail; got exit 0: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "TIMEOUT") && !strings.Contains(buf.String(), "timeout") {
		t.Logf("note: expected a timeout-class error, got %s", buf.String())
	}
}
