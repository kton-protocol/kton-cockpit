//go:build !unix

package watch

import "os"

// alive where there are no signals: finding a process opens it, which fails once it is gone
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
