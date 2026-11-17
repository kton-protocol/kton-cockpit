//go:build linux

package main

import (
	"os"
	"syscall"
	"time"
)

// atime: when a file was last read (relatime: at least once a day), else its modification time
func atime(fi os.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atim.Sec, st.Atim.Nsec)
	}
	return fi.ModTime()
}
