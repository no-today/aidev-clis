package tcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

func TestParseEnvelope_Data(t *testing.T) {
	r, err := parseEnvelope([]byte(`{"data":{"status_code":200}}`), nil)
	if err != nil || !r.HasData {
		t.Fatalf("want data, got %+v err %v", r, err)
	}
}

func TestParseEnvelope_DataDespiteNonZeroExit(t *testing.T) {
	// apicli 业务失败:exit!=0 但 stdout 有合法 data。必须按信封,不按退出码。
	r, err := parseEnvelope([]byte(`{"data":{"status_code":500,"ok":false}}`), errs.Remote("EXEC_FAILED", "exit 1"))
	if err != nil || !r.HasData {
		t.Fatalf("must honor data envelope over exit code: %+v err %v", r, err)
	}
}

func TestParseEnvelope_Error(t *testing.T) {
	r, err := parseEnvelope([]byte(`{"error":{"code":"TARGET_NOT_FOUND","message":"nope"}}`), errors.New("exit 2"))
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if r.HasData || r.ErrCode != "TARGET_NOT_FOUND" {
		t.Fatalf("want error envelope, got %+v", r)
	}
}

func TestParseEnvelope_Unparseable(t *testing.T) {
	_, err := parseEnvelope([]byte("panic: boom\n"), errors.New("exit 2"))
	if err == nil {
		t.Fatal("unparseable stdout must yield error")
	}
}

// TestSiblingCandidates_Windows verifies that on a simulated windows GOOS the
// resolver tries "<name>.exe" first (so dbcli.exe next to tcli.exe is found),
// then falls back to the bare name.
func TestSiblingCandidates_Windows(t *testing.T) {
	got := siblingCandidates("bin", "dbcli", "windows")
	if len(got) != 2 {
		t.Fatalf("want 2 candidates on windows, got %v", got)
	}
	if filepath.Base(got[0]) != "dbcli.exe" {
		t.Fatalf("want dbcli.exe first, got %q", got[0])
	}
	if filepath.Base(got[1]) != "dbcli" {
		t.Fatalf("want bare dbcli fallback, got %q", got[1])
	}
}

// TestSiblingCandidates_WindowsAlreadyExe ensures no double ".exe.exe" suffix.
func TestSiblingCandidates_WindowsAlreadyExe(t *testing.T) {
	got := siblingCandidates("bin", "dbcli.exe", "windows")
	if len(got) != 1 || filepath.Base(got[0]) != "dbcli.exe" {
		t.Fatalf("want single dbcli.exe, got %v", got)
	}
}

// TestSiblingCandidates_POSIX verifies POSIX keeps the bare name only.
func TestSiblingCandidates_POSIX(t *testing.T) {
	got := siblingCandidates("bin", "dbcli", "linux")
	if len(got) != 1 || filepath.Base(got[0]) != "dbcli" {
		t.Fatalf("want single bare dbcli on posix, got %v", got)
	}
}

// TestResolveSiblingCLI_FindsExe simulates a windows layout: a stub "dbcli.exe"
// placed next to a fake executable dir; siblingCandidates must surface it.
func TestResolveSiblingCLI_FindsExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "dbcli.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	for _, cand := range siblingCandidates(dir, "dbcli", "windows") {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			if cand == exe {
				return
			}
		}
	}
	t.Fatalf("dbcli.exe not resolved among candidates")
}

// Ensure CLIRunner interface is satisfied (compile-time check).
var _ CLIRunner = localCLI{}

// Stub to satisfy compile check for unused import.
var _ = context.Background
