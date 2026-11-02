// Package nofollow opens a file without following a symbolic link at its last element, on every
// platform the cockpit builds for.
package nofollow

import "os"

// OpenFile is os.OpenFile that refuses to follow a link at name itself. On unix this is
// O_NOFOLLOW, which the kernel applies atomically. Windows has no such flag, so a link found by
// Lstat beforehand is refused; that check and the open are two steps, which is the most the
// platform offers here.
func OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return openFile(name, flag, perm)
}
