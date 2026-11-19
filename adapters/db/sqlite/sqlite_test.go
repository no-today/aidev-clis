package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func newEnv(t *testing.T, mode string) {
	t.Helper()
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "app.db")
	// seed with the modernc driver directly
	seed(t, dbpath)
	t.Setenv("AIDEV_CLIS_HOME", dir)
	dsn := "file:" + dbpath
	if mode != "" {
		dsn += "?mode=" + mode
	}
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: sqlite\n    dsn: " + dsn + "\n"
	if err := os.WriteFile(filepath.Join(dir, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, allowWrite bool, args ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	dbcli.Run(context.Background(), reg, dbcli.RunArgs{Driver: "sqlite", Target: "it", AllowWrite: allowWrite, Args: args, Out: &buf})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %s", buf.String())
	}
	return env
}

func TestSqlite_ReadWriteDiscovery(t *testing.T) {
	newEnv(t, "")
	// read
	env := run(t, false, "SELECT id, name FROM users ORDER BY id")
	rows := env["data"].(map[string]any)["rows"].([]any)
	if len(rows) != 2 || rows[0].([]any)[1] != "alice" {
		t.Fatalf("read: %v", env)
	}
	// write blocked without --allow-write
	if run(t, false, "DELETE FROM users")["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatal("want WRITE_NOT_ALLOWED")
	}
	// write with --allow-write
	if run(t, true, "INSERT INTO users VALUES (3,'carol')")["data"].(map[string]any)["affected"].(float64) != 1 {
		t.Fatal("insert affected")
	}
	// discovery
	if len(run(t, false, "tables")["data"].(map[string]any)["rows"].([]any)) < 1 {
		t.Fatal("tables empty")
	}
	d := run(t, false, "describe", "users")["data"].(map[string]any)
	cols := d["columns"].([]any)
	if len(cols) != 2 || cols[0].(map[string]any)["name"] != "id" {
		t.Fatalf("describe: %v", d)
	}
	dchecks := run(t, false, "doctor")["data"].([]any)
	if dchecks[len(dchecks)-1].(map[string]any)["ok"] != true {
		t.Fatal("doctor")
	}
}

// --database is meaningless for a sqlite raw statement (no USE); rather than
// silently ignore it, the driver must surface a clear SQLITE_NO_DATABASE error —
// but only when --database is actually set.
func TestSqlite_DatabaseOnStatementErrors(t *testing.T) {
	newEnv(t, "")
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	dbcli.Run(context.Background(), reg, dbcli.RunArgs{
		Driver: "sqlite", Target: "it", Database: "other",
		Args: []string{"SELECT id FROM users"}, Out: &buf,
	})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %s", buf.String())
	}
	ee, ok := env["error"].(map[string]any)
	if !ok || ee["code"] != "SQLITE_NO_DATABASE" {
		t.Fatalf("want error.code=SQLITE_NO_DATABASE, got %v", env)
	}
	// Without --database the same statement runs fine (no-op path).
	if run(t, false, "SELECT id FROM users")["error"] != nil {
		t.Fatal("statement without --database must not error")
	}
	// The tables verb legitimately uses --database and must NOT be broken by the
	// guard (it targets schema-qualified catalog queries, not UseDatabase).
	env = run(t, false, "tables", "main")
	if env["error"] != nil {
		t.Fatalf("tables --database main must work: %v", env)
	}
}

func TestSqlite_ReadOnlyMode(t *testing.T) {
	newEnv(t, "ro")
	// the driver rejects writes when opened ?mode=ro (the security boundary)
	if run(t, true, "INSERT INTO users VALUES (9,'x')")["error"] == nil {
		t.Fatal("mode=ro must reject the write at the driver")
	}
}

func seed(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO users VALUES (1,'alice'),(2,'bob')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
}
