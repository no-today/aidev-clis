//go:build integration

package redis

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeEnvDocker points the "it" env at the docker redis. The shared `run`
// helper (redis_test.go) drives the command.
func writeEnvDocker(t *testing.T) {
	t.Helper()
	if c, err := net.DialTimeout("tcp", "127.0.0.1:16379", time.Second); err != nil {
		t.Skip("redis not reachable on 127.0.0.1:16379; run make db-up")
	} else {
		_ = c.Close()
	}
	home := t.TempDir()
	t.Setenv("AIDEV_CLIS_HOME", home)
	cfg := "default_target: it\ntargets:\n  it:\n    adapter: redis\n    dsn: redis://127.0.0.1:16379/0\n"
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIT_Redis_RoundTrip(t *testing.T) {
	writeEnvDocker(t)
	if run(t, true, "SET", "it:k", "hello")["error"] != nil {
		t.Fatal("SET failed")
	}
	env := run(t, false, "GET", "it:k")
	if env["data"].(map[string]any)["rows"].([]any)[0].([]any)[0] != "hello" {
		t.Fatalf("GET: %v", env)
	}
	if run(t, false, "SET", "it:k", "x")["error"].(map[string]any)["code"] != "WRITE_NOT_ALLOWED" {
		t.Fatal("want WRITE_NOT_ALLOWED")
	}
	if run(t, true, "FLUSHALL")["error"].(map[string]any)["code"] != "REDIS_ADMIN_REFUSED" {
		t.Fatal("want REDIS_ADMIN_REFUSED")
	}
	if run(t, false, "describe", "it:k")["data"].(map[string]any)["type"] != "string" {
		t.Fatal("describe type")
	}
	dchecks := run(t, false, "doctor")["data"].([]any)
	if dchecks[len(dchecks)-1].(map[string]any)["ok"] != true {
		t.Fatal("doctor")
	}
	_ = run(t, true, "DEL", "it:k")
}
