//go:build windows

package audit

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f's first byte range (enough to serialize
// appends), returning an unlock func.
func lockFile(f *os.File) (func(), error) {
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(h, 0, 1, 0, ol) }, nil
}
