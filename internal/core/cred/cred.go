// Package cred reads credential files from $AIDEV_CLIS_HOME/credentials/<name>
// (or ~/.aidev-clis/credentials/<name>). The "permissions too open" check is
// build-tag split (see perm_unix.go / perm_windows.go) because POSIX mode bits
// don't apply on Windows.
package cred

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/config"
	"github.com/no-today/aidev-clis/internal/core/errs"
)

// check validates the name, opens the credential file, and runs the perm check
// against the open fd (no stat-then-read TOCTOU). Returns the open file + path;
// the caller must Close it.
func check(name string) (string, *os.File, error) {
	if name == "" || name == "." || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", nil, errs.Config("CRED_BAD_NAME", fmt.Sprintf("invalid credential name %q", name))
	}
	home, err := config.Home()
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(home, "credentials", name)
	f, err := os.Open(path)
	if err != nil {
		return "", nil, errs.Auth("CRED_MISSING", fmt.Sprintf("credential %q not found at %s", name, path))
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return "", nil, errs.Auth("CRED_READ_FAILED", err.Error())
	}
	if tooOpen(info.Mode()) {
		_ = f.Close()
		return "", nil, errs.Auth("CRED_PERMS_TOO_OPEN",
			fmt.Sprintf("credential %s must be 0600 (group/other must not access it)", path))
	}
	return path, f, nil
}

// Read returns the bytes of the named credential (for credentials consumed as
// content, e.g. AK/SK JSON).
func Read(name string) ([]byte, error) {
	_, f, err := check(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, errs.Auth("CRED_READ_FAILED", err.Error())
	}
	return b, nil
}

// Path returns the validated filesystem path of the named credential (for
// credentials a subprocess reads as a FILE, e.g. a kubeconfig set via
// KUBECONFIG). Same name-validation + perm check as Read. The AI never sees
// the path or contents — the CLI sets it on the subprocess env.
func Path(name string) (string, error) {
	path, f, err := check(name)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	return path, nil
}
