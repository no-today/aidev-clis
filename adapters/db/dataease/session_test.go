package dataease

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveSession_WritesSessionsDir0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	sess := &Session{
		Token:      "jwt-token",
		BaseURL:    "https://example.test/dataease",
		SessionKey: "dataease.local.session",
	}

	path, err := SaveSession(sess, "dataease.local.session", dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "dataease.local.session"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	// POSIX perms are a no-op on Windows (ACL-governed); assert them only on unix.
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestSaveLoadSession_RoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	base := "https://example.test/dataease"
	if _, err := SaveSession(&Session{Token: "jwt", BaseURL: base}, "dataease.local.session", dir); err != nil {
		t.Fatal(err)
	}
	sess, err := LoadSession("dataease.local.session", base, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token != "jwt" {
		t.Errorf("Token = %q", sess.Token)
	}
}

func TestLoadSession_MissingFileIsSessionMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	_, err := LoadSession("dataease.local.session", "https://example.test/dataease", dir)
	requireCode(t, err, "DATAEASE_SESSION_MISSING")
}

func TestLoadSession_RejectsMissingToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dataease.local.session"), []byte(`{"token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSession("dataease.local.session", "https://example.test/dataease", dir)
	requireCode(t, err, "DATAEASE_SESSION_NO_TOKEN")
}

func TestLoadSession_RejectsBaseURLMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"token":"jwt-token","base_url":"https://one.example/dataease","session_key":"dataease.local.session"}`)
	if err := os.WriteFile(filepath.Join(dir, "dataease.local.session"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSession("dataease.local.session", "https://two.example/dataease", dir)
	requireCode(t, err, "DATAEASE_SESSION_BASE_URL_MISMATCH")
}

func TestSessionName_RejectsPathTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	_, err := SaveSession(&Session{Token: "jwt-token"}, "../bad", dir)
	requireCode(t, err, "CRED_BAD_NAME")

	_, err = LoadSession("../bad", "https://example.test/dataease", dir)
	requireCode(t, err, "CRED_BAD_NAME")
}
