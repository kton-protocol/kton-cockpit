//go:build windows

package oxigraph

import "os"

// alive says whether a process of that id runs: Windows opens a handle only to a process that exists.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
