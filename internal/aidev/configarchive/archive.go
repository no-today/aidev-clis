// Package configarchive creates and restores shareable aidev config bundles.
package configarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

const manifestName = "manifest.json"

// Entry describes one config payload file inside an archive.
type Entry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

// ArchiveResult is emitted by export and backup operations.
type ArchiveResult struct {
	Path    string  `json:"path"`
	Root    string  `json:"root"`
	Count   int     `json:"count"`
	Entries []Entry `json:"entries,omitempty"`
}

// RestoreResult is emitted by restore/import operations.
type RestoreResult struct {
	Archive    string  `json:"archive"`
	Root       string  `json:"root"`
	BackupPath string  `json:"backup_path,omitempty"`
	Count      int     `json:"count"`
	Entries    []Entry `json:"entries,omitempty"`
}

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// Backup creates a safety backup of the target root before writing files.
	Backup bool
	// BackupDir overrides the default <root>/backups safety-backup location.
	BackupDir string
	// Now makes archive names deterministic for tests. Zero means time.Now().
	Now time.Time
}

type fileEntry struct {
	Entry
	absPath string
}

type restoreFile struct {
	Entry
	body []byte
}

type manifest struct {
	Version   int     `json:"version"`
	CreatedAt string  `json:"created_at"`
	Entries   []Entry `json:"entries"`
}

// Create writes a tar.gz archive containing top-level *.yaml files and every
// regular file under credentials/. Existing destination files are not
// overwritten.
func Create(root, dest string, now time.Time) (*ArchiveResult, error) {
	root = filepath.Clean(root)
	dest = filepath.Clean(dest)
	if err := ensureRoot(root); err != nil {
		return nil, err
	}
	entries, err := collect(root)
	if err != nil {
		return nil, err
	}
	if err := writeArchive(dest, entries, normalizedTime(now)); err != nil {
		return nil, err
	}
	return &ArchiveResult{
		Path:    dest,
		Root:    root,
		Count:   len(entries),
		Entries: publicEntries(entries),
	}, nil
}

// Backup writes a timestamped archive. The default destination directory is
// <root>/backups.
func Backup(root, backupDir string, now time.Time) (*ArchiveResult, error) {
	root = filepath.Clean(root)
	if backupDir == "" {
		backupDir = filepath.Join(root, "backups")
	}
	ts := normalizedTime(now).Format("20060102-150405")
	dest, err := nextBackupPath(backupDir, ts)
	if err != nil {
		return nil, err
	}
	return Create(root, dest, now)
}

// Restore imports an archive into root. It validates the full archive before
// making the optional safety backup or writing any payload file.
func Restore(root, archive string, opts RestoreOptions) (*RestoreResult, error) {
	root = filepath.Clean(root)
	archive = filepath.Clean(archive)
	if err := ensureRoot(root); err != nil {
		return nil, err
	}
	files, err := readArchive(archive)
	if err != nil {
		return nil, err
	}

	backupPath := ""
	if opts.Backup {
		backup, err := Backup(root, opts.BackupDir, opts.Now)
		if err != nil {
			return nil, err
		}
		backupPath = backup.Path
	}

	for _, file := range files {
		if err := restoreOne(root, file); err != nil {
			return nil, err
		}
	}
	return &RestoreResult{
		Archive:    archive,
		Root:       root,
		BackupPath: backupPath,
		Count:      len(files),
		Entries:    restoreEntries(files),
	}, nil
}

func ensureRoot(root string) error {
	if root == "" || root == "." || root == string(filepath.Separator) {
		return errs.Config("CONFIG_ROOT_INVALID",
			fmt.Sprintf("refusing dangerous config root %q", root))
	}
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errs.Config("CONFIG_ROOT_SYMLINK",
				fmt.Sprintf("config root %s must not be a symlink", root))
		}
		if !info.IsDir() {
			return errs.Config("CONFIG_ROOT_NOT_DIR",
				fmt.Sprintf("config root %s is not a directory", root))
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(root, 0o700)
}

func collect(root string) ([]fileEntry, error) {
	var entries []fileEntry
	topLevel, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, item := range topLevel {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		entries = append(entries, fileEntry{
			Entry:   Entry{Path: item.Name(), Size: info.Size(), Mode: formatMode(info.Mode())},
			absPath: filepath.Join(root, item.Name()),
		})
	}

	credRoot := filepath.Join(root, "credentials")
	if info, err := os.Lstat(credRoot); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errs.Config("CONFIG_CREDENTIALS_SYMLINK",
				fmt.Sprintf("credentials directory %s must not be a symlink", credRoot))
		}
		if !info.IsDir() {
			return nil, errs.Config("CONFIG_CREDENTIALS_NOT_DIR",
				fmt.Sprintf("credentials path %s is not a directory", credRoot))
		}
		err := filepath.WalkDir(credRoot, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			entries = append(entries, fileEntry{
				Entry:   Entry{Path: filepath.ToSlash(rel), Size: info.Size(), Mode: formatMode(info.Mode())},
				absPath: p,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func writeArchive(dest string, entries []fileEntry, now time.Time) error {
	if _, err := os.Stat(dest); err == nil {
		return errs.Config("CONFIG_ARCHIVE_EXISTS",
			fmt.Sprintf("archive %s already exists", dest))
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	if err := writeManifest(tw, publicEntries(entries), now); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = tmp.Close()
		return err
	}
	for _, entry := range entries {
		if err := writeFile(tw, entry, now); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = tmp.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		_ = tmp.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func writeManifest(tw *tar.Writer, entries []Entry, now time.Time) error {
	body, err := json.MarshalIndent(manifest{
		Version:   1,
		CreatedAt: now.UTC().Format(time.RFC3339),
		Entries:   entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeBytes(tw, manifestName, body, 0o600, now)
}

func writeFile(tw *tar.Writer, entry fileEntry, now time.Time) error {
	data, err := os.ReadFile(entry.absPath)
	if err != nil {
		return err
	}
	mode := parseMode(entry.Mode, 0o600)
	if strings.HasPrefix(entry.Path, "credentials/") {
		mode = 0o600
	}
	return writeBytes(tw, entry.Path, data, mode, now)
}

func writeBytes(tw *tar.Writer, name string, body []byte, mode os.FileMode, now time.Time) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    int64(mode.Perm()),
		Size:    int64(len(body)),
		ModTime: now,
	}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func readArchive(archive string) ([]restoreFile, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, errs.Config("CONFIG_ARCHIVE_INVALID",
			fmt.Sprintf("archive %s is not a gzip tar archive: %v", archive, err))
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var files []restoreFile
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errs.Config("CONFIG_ARCHIVE_INVALID",
				fmt.Sprintf("archive %s cannot be read: %v", archive, err))
		}
		if hdr.Name == manifestName {
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return nil, err
			}
			continue
		}
		clean, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			if clean != "credentials" && !strings.HasPrefix(clean, "credentials/") {
				return nil, errs.Config("CONFIG_ARCHIVE_UNSUPPORTED_PATH",
					fmt.Sprintf("archive directory %q is outside the aidev config payload", clean))
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != 0 {
			return nil, errs.Config("CONFIG_ARCHIVE_UNSUPPORTED_TYPE",
				fmt.Sprintf("archive entry %q has unsupported type %d", clean, hdr.Typeflag))
		}
		if !allowedPayloadPath(clean) {
			return nil, errs.Config("CONFIG_ARCHIVE_UNSUPPORTED_PATH",
				fmt.Sprintf("archive entry %q is outside the aidev config payload", clean))
		}
		if seen[clean] {
			return nil, errs.Config("CONFIG_ARCHIVE_DUPLICATE_PATH",
				fmt.Sprintf("archive entry %q appears more than once", clean))
		}
		seen[clean] = true
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			return nil, err
		}
		mode := sanitizeRestoreMode(clean, os.FileMode(hdr.Mode))
		files = append(files, restoreFile{
			Entry: Entry{Path: clean, Size: int64(buf.Len()), Mode: formatMode(mode)},
			body:  buf.Bytes(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func restoreOne(root string, file restoreFile) error {
	if err := ensureSafeParent(root, file.Path); err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(file.Path))
	mode := parseMode(file.Mode, 0o600)
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(file.body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func ensureSafeParent(root, rel string) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(dir, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errs.Config("CONFIG_RESTORE_PARENT_SYMLINK",
				fmt.Sprintf("restore parent %s must not be a symlink", current))
		}
		if !info.IsDir() {
			return errs.Config("CONFIG_RESTORE_PARENT_NOT_DIR",
				fmt.Sprintf("restore parent %s is not a directory", current))
		}
	}
	return nil
}

func cleanArchivePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", errs.Config("CONFIG_ARCHIVE_INVALID_PATH",
			fmt.Sprintf("invalid archive path %q", name))
	}
	clean := path.Clean(name)
	if clean != name || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errs.Config("CONFIG_ARCHIVE_INVALID_PATH",
			fmt.Sprintf("invalid archive path %q", name))
	}
	return clean, nil
}

func allowedPayloadPath(name string) bool {
	if !strings.Contains(name, "/") {
		return strings.HasSuffix(name, ".yaml")
	}
	return strings.HasPrefix(name, "credentials/") && name != "credentials/"
}

func nextBackupPath(dir, ts string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := "aidev-config-" + ts
	for i := 0; i < 1000; i++ {
		name := base + ".tar.gz"
		if i > 0 {
			name = fmt.Sprintf("%s-%03d.tar.gz", base, i)
		}
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errs.Config("CONFIG_BACKUP_COLLISION",
		fmt.Sprintf("could not find an unused backup name in %s for timestamp %s", dir, ts))
}

func sanitizeRestoreMode(name string, mode os.FileMode) os.FileMode {
	if strings.HasPrefix(name, "credentials/") {
		return 0o600
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o600
	}
	return perm &^ 0o022
}

func normalizedTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now()
	}
	return now
}

func publicEntries(entries []fileEntry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Entry)
	}
	return out
}

func restoreEntries(files []restoreFile) []Entry {
	out := make([]Entry, 0, len(files))
	for _, file := range files {
		out = append(out, file.Entry)
	}
	return out
}

func formatMode(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}

func parseMode(raw string, fallback os.FileMode) os.FileMode {
	var mode uint32
	if _, err := fmt.Sscanf(raw, "%o", &mode); err != nil {
		return fallback
	}
	return os.FileMode(mode).Perm()
}
