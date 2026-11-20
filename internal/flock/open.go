package flock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// open opens the lock file, making its folder. Another user's lock file this one may not write is
// opened read-only: an exclusive lock does not need a writable handle, so two users of one checkout
// still take the same lock (kton-review X1 on #183).
func open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// O_NOFOLLOW: a symlink planted at the lock's name is not followed to create or open another file
	// (kton-review N5 on #183).
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|oNoFollow, 0o666)
	if errors.Is(err, fs.ErrPermission) {
		return os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	}
	return f, err
}

// InCheckout is the lock file name for the checkout at root: .kton/local/<name>.lock, the checkout's
// own and never shared, so every process writing to the checkout takes the same file, whatever its
// TMPDIR (kton-review X1 on #183).
func InCheckout(root, name string) string {
	return filepath.Join(root, ".kton", "local", name+".lock")
}
