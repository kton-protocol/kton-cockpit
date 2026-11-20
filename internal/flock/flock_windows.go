//go:build windows

package flock

import (
	"golang.org/x/sys/windows"
)

// Windows has no O_NOFOLLOW.
const oNoFollow = 0

// Lock holds an exclusive lock on path until the returned function is called.
func Lock(path string) (func(), error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() { windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol); f.Close() }, nil
}
