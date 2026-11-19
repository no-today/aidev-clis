//go:build !windows

package audit

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive advisory lock on f, returning an unlock func.
func lockFile(f *os.File) (func(), error) {
	fd := int(f.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return nil, err
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN) }, nil
}
