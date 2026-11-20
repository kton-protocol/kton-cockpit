//go:build !windows

package flock

import (
	"syscall"
)

// Lock holds an exclusive lock on path until the returned function is called. Two cockpits
// starting the daemon, or reading one registry into it, take turns.
func Lock(path string) (func(), error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
