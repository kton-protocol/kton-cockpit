//go:build unix

package watch

import (
	"errors"
	"syscall"
)

// alive: signal 0 reaches the process, or it exists but belongs to someone else
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
