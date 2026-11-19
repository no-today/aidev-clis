//go:build unix

package cred

import "os"

// tooOpen reports whether the file is accessible by group or other.
func tooOpen(m os.FileMode) bool { return m.Perm()&0o077 != 0 }
