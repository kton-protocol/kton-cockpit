//go:build !windows

package oxigraph

import (
	"os"
	"syscall"
)

// flock holds an exclusive lock on path until the returned function is called. Two cockpits
// starting the daemon, or reading one registry into it, take turns.
func flock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
