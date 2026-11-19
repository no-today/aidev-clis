package configarchive

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 6, 12, 11, 4, 5, 0, time.UTC) }

func entryPaths(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

func tarNames(t *testing.T, archive string) []string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	return names
}

func mustWrite(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCreateIncludesOnlyTopLevelYAMLAndCredentials(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "dbcli.yaml"), "default_target: dev\n", 0o644)
	mustWrite(t, filepath.Join(root, "actors.yaml"), "actors: {}\n", 0o600)
	mustWrite(t, filepath.Join(root, "audit.jsonl"), "{}\n", 0o600)             // excluded
	mustWrite(t, filepath.Join(root, "audit", "20260701.jsonl"), "{}\n", 0o600) // excluded (audit dir)
	mustWrite(t, filepath.Join(root, "sessions", "api.session"), "s", 0o600)    // excluded
	mustWrite(t, filepath.Join(root, "credentials", "db.dev.dsn"), "secret", 0o600)

	dest := filepath.Join(t.TempDir(), "company-aidev-config.tar.gz")
	result, err := Create(root, dest, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != dest {
		t.Errorf("path = %s, want %s", result.Path, dest)
	}
	want := []string{"actors.yaml", "credentials/db.dev.dsn", "dbcli.yaml"}
	if got := entryPaths(result.Entries); !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %o, want 600", info.Mode().Perm())
	}
	wantTar := []string{"manifest.json", "actors.yaml", "credentials/db.dev.dsn", "dbcli.yaml"}
	if got := tarNames(t, dest); !reflect.DeepEqual(got, wantTar) {
		t.Errorf("tar names = %v, want %v", got, wantTar)
	}
}

func TestBackupUsesTimestampedArchiveUnderConfigRoot(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "logcli.yaml"), "default_target: dev\n", 0o644)

	result, err := Backup(root, "", fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "backups", "aidev-config-20260612-110405.tar.gz")
	if result.Path != want {
		t.Errorf("path = %s, want %s", result.Path, want)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreCreatesSafetyBackupAndRestoresPayload(t *testing.T) {
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "dbcli.yaml"), "default_target: restored\n", 0o644)
	mustWrite(t, filepath.Join(source, "credentials", "db.dev.dsn"), "restored-secret", 0o600)
	archive := filepath.Join(t.TempDir(), "shared.tar.gz")
	if _, err := Create(source, archive, fixedNow()); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	mustWrite(t, filepath.Join(target, "dbcli.yaml"), "default_target: broken\n", 0o644)
	mustWrite(t, filepath.Join(target, "credentials", "db.dev.dsn"), "old-secret", 0o644)

	result, err := Restore(target, archive, RestoreOptions{Backup: true, Now: fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	wantEntries := []string{"credentials/db.dev.dsn", "dbcli.yaml"}
	if got := entryPaths(result.Entries); !reflect.DeepEqual(got, wantEntries) {
		t.Errorf("entries = %v, want %v", got, wantEntries)
	}
	if result.BackupPath == "" {
		t.Error("want a safety backup path")
	}
	if readFile(t, filepath.Join(target, "dbcli.yaml")) != "default_target: restored\n" {
		t.Error("dbcli.yaml not restored")
	}
	if readFile(t, filepath.Join(target, "credentials", "db.dev.dsn")) != "restored-secret" {
		t.Error("credential not restored")
	}
	info, err := os.Stat(filepath.Join(target, "credentials", "db.dev.dsn"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("credential mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRestoreRejectsPathTraversalArchive(t *testing.T) {
	// Hand-craft a malicious archive with a ../ entry.
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("pwn")
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.yaml", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	target := t.TempDir()
	if _, err := Restore(target, archive, RestoreOptions{Backup: false}); err == nil {
		t.Fatal("want error for path-traversal archive")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), "escape.yaml")); err == nil {
		t.Fatal("traversal wrote a file outside root")
	}
}

func TestBackupNoBackupSkipsSafety(t *testing.T) {
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "dbcli.yaml"), "x: 1\n", 0o644)
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if _, err := Create(source, archive, fixedNow()); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	result, err := Restore(target, archive, RestoreOptions{Backup: false, Now: fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath != "" {
		t.Errorf("backup_path = %q, want empty with Backup:false", result.BackupPath)
	}
	if _, err := os.Stat(filepath.Join(target, "backups")); !os.IsNotExist(err) {
		t.Error("no-backup restore should not create backups/")
	}
}
