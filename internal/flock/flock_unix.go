//go:build !windows

package flock

import (
	"syscall"
)

const oNoFollow = syscall.O_NOFOLLOW

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

// TryLock takes the lock on path if no one holds it, without waiting: held is false when another
// process holds it. A build's marker uses it to tell a running build from one that was killed.
func TryLock(path string) (unlock func(), held bool, err error) {
	f, err := open(path)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, false, nil
}
