//go:build !windows

package oxigraph

import "syscall"

// alive says whether a process of that id runs (signal 0 tests without sending).
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
