//go:build docker && unix

package tools

import (
	"io/fs"
	"os"
	"syscall"
	"testing"
)

func ownedByCurrentUser(t *testing.T, fi fs.FileInfo) bool {
	t.Helper()
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no unix stat available")
	}
	return int(st.Uid) == os.Getuid()
}
