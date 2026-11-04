//go:build !unix

package nofollow

import (
	"fmt"
	"os"
)

func openFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if fi, err := os.Lstat(name); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, &os.PathError{Op: "open", Path: name, Err: fmt.Errorf("is a symbolic link, not followed")}
	}
	return os.OpenFile(name, flag, perm)
}
