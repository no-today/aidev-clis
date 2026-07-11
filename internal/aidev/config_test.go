package aidev

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "dbcli.yaml"), []byte("default_target: dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIDEV_CLIS_HOME", home)
	return home
}

func TestRunConfigBackupThenRestore(t *testing.T) {
	home := configHome(t)

	var buf bytes.Buffer
	if code := RunConfig(&buf, []string{"backup"}); code != 0 {
		t.Fatalf("backup code = %d, out = %s", code, buf.String())
	}
	var env struct {
		Data struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if env.Data.Count != 1 || !strings.HasPrefix(env.Data.Path, filepath.Join(home, "backups")) {
		t.Fatalf("unexpected backup data: %+v", env.Data)
	}

	buf.Reset()
	if code := RunConfig(&buf, []string{"restore", env.Data.Path}); code != 0 {
		t.Fatalf("restore code = %d, out = %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), `"backup_path"`) {
		t.Errorf("restore should report a safety backup_path: %s", buf.String())
	}
}

func TestRunConfigUnknownSubcommand(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	var buf bytes.Buffer
	code := RunConfig(&buf, []string{"frobnicate"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(buf.String(), "UNSUPPORTED_SUBCOMMAND") {
		t.Errorf("want UNSUPPORTED_SUBCOMMAND, got %s", buf.String())
	}
}

func TestRunConfigRestoreMissingArg(t *testing.T) {
	t.Setenv("AIDEV_CLIS_HOME", t.TempDir())
	var buf bytes.Buffer
	code := RunConfig(&buf, []string{"restore"})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(buf.String(), "MISSING_ARG") {
		t.Errorf("want MISSING_ARG, got %s", buf.String())
	}
}

func TestRunConfigRestoreNoBackupRaw(t *testing.T) {
	configHome(t)
	var buf bytes.Buffer
	if code := RunConfig(&buf, []string{"backup"}); code != 0 {
		t.Fatal(buf.String())
	}
	var env struct {
		Data struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	_ = json.Unmarshal(buf.Bytes(), &env)

	buf.Reset()
	code := RunConfig(&buf, []string{"restore", "--no-backup", "--output", "raw", env.Data.Path})
	if code != 0 {
		t.Fatalf("code = %d, out = %s", code, buf.String())
	}
	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("raw output should not be JSON: %s", out)
	}
	if !strings.Contains(out, "restored") {
		t.Errorf("raw restore output should mention 'restored': %s", out)
	}
	if strings.Contains(out, "safety backup:") {
		t.Errorf("--no-backup should not report a safety backup: %s", out)
	}
}
