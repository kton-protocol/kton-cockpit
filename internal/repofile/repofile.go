// Package repofile reads a file a request names, for its hash, without being led outside the
// repository or into a read that never ends. It is the one resolver for say's subject, ask's file
// reference and the shell's references (kton-review S1/S2 on #148).
package repofile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/nofollow"
)

// ErrNotFound is a name that is no file at all, so a caller can try another reading of it.
var ErrNotFound = errors.New("no such file")

// Hash returns the sha256 of the regular file path names, and the path as reported back (relative to
// root, slash-separated, as the caller gave it). path is relative to root unless absolute.
//
// Symbolic links are resolved first and containment is checked on the resolved paths: a link in the
// repository to a file outside it names that outside file. With inside, such a file is refused; a
// caller that signs what it hashes needs that, since the claim is shared and a low-entropy file's hash
// can be guessed back. Only a regular file is read: a directory, a FIFO or a device is refused before
// it is opened, and the open neither follows a link nor blocks, so a file swapped for a FIFO after the
// check cannot hang the read.
func Hash(root, path string, inside bool) (hash, reported string, err error) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(path))
	}
	outside := func(rel string, err error) bool {
		return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if inside && outside(filepath.Rel(root, abs)) {
		return "", "", fmt.Errorf("%q is outside this repository", path)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	if inside {
		if outside(filepath.Rel(realRoot, real)) {
			return "", "", fmt.Errorf("%q is outside this repository (a link to %s)", path, real)
		}
	}
	fi, err := os.Lstat(real)
	if err != nil {
		return "", "", err
	}
	switch {
	case fi.IsDir():
		return "", "", fmt.Errorf("%q is a directory", path)
	case !fi.Mode().IsRegular():
		return "", "", fmt.Errorf("%q is not a regular file (%s)", path, fi.Mode().Type())
	}
	f, err := openRegular(real)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(fi, opened) {
		return "", "", fmt.Errorf("%q changed while it was read", path)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", "", err
	}
	reported = filepath.ToSlash(path)
	if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		reported = filepath.ToSlash(rel)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), reported, nil
}

// openRegular opens name for reading without following a link at it and without blocking.
func openRegular(name string) (*os.File, error) {
	return nofollow.OpenFile(name, os.O_RDONLY|nonblock, 0)
}
