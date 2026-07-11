//go:build integration

package pg

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func writeEnv(t *testing.T, user, pass string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	_ = os.MkdirAll(filepath.Join(home, "credentials"), 0o700)
	_ = os.WriteFile(filepath.Join(home, "credentials", "db.pw"), []byte(pass), 0o600)
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: postgres\n    dsn: postgres://" + user + "@127.0.0.1:15432/bizdb\n    credential: db.pw\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, allowWrite bool, db string, args ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	dbcli.Run(context.Background(), reg, dbcli.RunArgs{Driver: "postgres", Target: "it", Database: db, AllowWrite: allowWrite, Args: args, Out: &buf})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %s", buf.String())
	}
	return env
}

func TestIT_PG_MultiSchemaDiscovery(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "", "databases")
	rows := env["data"].(map[string]any)["rows"].([]any)
	var names []string
	for _, r := range rows {
		names = append(names, r.([]any)[0].(string))
	}
	if !has(names, "public") || !has(names, "sales") || has(names, "pg_catalog") {
		t.Fatalf("schemas: %v", names)
	}
}

func TestIT_PG_DescribeAmbiguous(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "", "describe", "orders")
	if env["error"] == nil || env["error"].(map[string]any)["code"] != "TABLE_AMBIGUOUS" {
		t.Fatalf("want TABLE_AMBIGUOUS, got %v", env)
	}
	env = run(t, false, "", "describe", "sales.orders")
	if env["error"] != nil {
		t.Fatalf("qualified describe failed: %v", env)
	}
}

func TestIT_PG_SchemaScopeAndRead(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "sales", "SELECT region FROM orders ORDER BY id")
	if env["error"] != nil {
		t.Fatalf("scoped read failed: %v", env)
	}
	cols := env["data"].(map[string]any)["columns"].([]any)
	if len(cols) != 1 || cols[0].(string) != "region" {
		t.Fatalf("expected sales.orders columns, got %v", env)
	}
}

func TestIT_PG_CoerceAndGuard(t *testing.T) {
	writeEnv(t, "app_ro", "ro_pw")
	env := run(t, false, "", "SELECT id, big, avatar FROM public.users WHERE id=1")
	row := env["data"].(map[string]any)["rows"].([]any)[0].([]any)
	if _, ok := row[1].(string); !ok {
		t.Fatalf("big should stringify: %T", row[1])
	}
	if _, ok := row[2].(string); !ok {
		t.Fatalf("bytea should be base64 string: %T", row[2])
	}
	env = run(t, false, "", "DELETE FROM public.users WHERE id=99")
	if env["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatalf("want WRITE_NOT_ALLOWED, got %v", env)
	}
}

func TestIT_PG_WriteWithRW(t *testing.T) {
	writeEnv(t, "app_rw", "rw_pw")
	env := run(t, true, "public", "UPDATE orders SET status='PAID' WHERE id=2")
	if env["data"].(map[string]any)["affected"].(float64) != 1 {
		t.Fatalf("affected: %v", env)
	}
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
