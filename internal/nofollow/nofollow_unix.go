//go:build unix

package nofollow

import (
	"os"
	"syscall"
)

func openFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag|syscall.O_NOFOLLOW, perm)
}
