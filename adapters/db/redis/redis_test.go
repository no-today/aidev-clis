package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/no-today/aidev-clis/internal/dbcli"
)

func setup(t *testing.T) (*miniredis.Miniredis, string) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: redis\n    dsn: redis://" + mr.Addr() + "/0\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return mr, home
}

func run(t *testing.T, allowWrite bool, args ...string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	reg := dbcli.NewRegistry([]dbcli.Driver{New()})
	dbcli.Run(context.Background(), reg, dbcli.RunArgs{Driver: "redis", Target: "it", AllowWrite: allowWrite, Args: args, Out: &buf})
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %s", buf.String())
	}
	return env
}

func TestRedis_ReadWriteGuard(t *testing.T) {
	mr, _ := setup(t)
	_ = mr.Set("k", "v")

	// read
	env := run(t, false, "GET", "k")
	if env["data"].(map[string]any)["rows"].([]any)[0].([]any)[0] != "v" {
		t.Fatalf("GET: %v", env)
	}
	// write blocked
	env = run(t, false, "SET", "k", "x")
	if env["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatalf("want WRITE_NOT_ALLOWED, got %v", env)
	}
	// write allowed → affected
	env = run(t, true, "DEL", "k")
	if env["data"].(map[string]any)["affected"].(float64) != 1 {
		t.Fatalf("DEL affected: %v", env)
	}
	// admin refused even with --allow-write
	env = run(t, true, "FLUSHALL")
	if env["error"].(map[string]any)["code"] != "REDIS_ADMIN_REFUSED" {
		t.Fatalf("want REDIS_ADMIN_REFUSED, got %v", env)
	}
}

// TestRedis_ReadRowCap covers review #1: a large multi-element reply (LRANGE)
// is capped to rowCap elements with a warning, while a scalar read (GET) is
// returned whole (never capped).
func TestRedis_ReadRowCap(t *testing.T) {
	mr, _ := setup(t)
	for i := 0; i < rowCap+50; i++ {
		if _, err := mr.Push("big", "e"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	env := run(t, false, "LRANGE", "big", "0", "-1")
	rows := env["data"].(map[string]any)["rows"].([]any)
	if len(rows) != rowCap {
		t.Fatalf("want %d rows, got %d", rowCap, len(rows))
	}
	warnings, ok := env["warnings"].([]any)
	if !ok {
		t.Fatalf("expected warnings, got %v", env)
	}
	var found bool
	for _, w := range warnings {
		if w == "result truncated to 100 elements" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing truncation warning: %v", warnings)
	}

	// scalar GET is not row-capped and carries no truncation warning
	_ = mr.Set("s", "v")
	env = run(t, false, "GET", "s")
	if env["data"].(map[string]any)["rows"].([]any)[0].([]any)[0] != "v" {
		t.Fatalf("GET: %v", env)
	}
	if _, ok := env["warnings"]; ok {
		t.Fatalf("scalar GET should not warn: %v", env)
	}
}

// TestRedis_QuotedValueSplit covers review #2: a whole-command string with a
// double-quoted value containing spaces splits into the right argv (SET k "a b"
// → 3 tokens, not 4).
func TestRedis_QuotedValueSplit(t *testing.T) {
	if got := splitCommand(`SET k "a b"`); !reflect.DeepEqual(got, []string{"SET", "k", "a b"}) {
		t.Fatalf("quoted split: %v", got)
	}
	if got := splitCommand("GET   k"); !reflect.DeepEqual(got, []string{"GET", "k"}) {
		t.Fatalf("plain split: %v", got)
	}

	mr, _ := setup(t)
	if env := run(t, true, `SET k "a b"`); env["data"].(map[string]any)["affected"].(float64) != 1 {
		t.Fatalf("SET quoted: %v", env)
	}
	if v, _ := mr.Get("k"); v != "a b" {
		t.Fatalf("stored value = %q, want %q", v, "a b")
	}
}

func TestRedis_HashAndVerbs(t *testing.T) {
	mr, _ := setup(t)
	mr.HSet("h", "f1", "v1", "f2", "v2")
	env := run(t, false, "HGETALL", "h")
	cols := env["data"].(map[string]any)["columns"].([]any)
	if cols[0] != "field" || cols[1] != "value" {
		t.Fatalf("HGETALL shape: %v", env)
	}
	// describe a key
	env = run(t, false, "describe", "h")
	if env["data"].(map[string]any)["type"] != "hash" {
		t.Fatalf("describe: %v", env)
	}
	// doctor — staged checks; the last (connect) stage must be ok
	env = run(t, false, "doctor")
	dchecks := env["data"].([]any)
	if dchecks[len(dchecks)-1].(map[string]any)["ok"] != true {
		t.Fatalf("doctor: %v", env)
	}
	// tables (SCAN)
	env = run(t, false, "tables")
	if _, ok := env["data"].(map[string]any)["rows"]; !ok {
		t.Fatalf("tables: %v", env)
	}
}
