//go:build windows

package cred

import "os"

// tooOpen is a no-op on Windows: access is governed by ACLs, not POSIX bits.
func tooOpen(os.FileMode) bool { return false }
