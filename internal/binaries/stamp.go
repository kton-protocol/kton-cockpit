package binaries

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Stamp names every file under the registries a union is built from, with its size and
// modification time. A registry that does not exist is part of the stamp too, so creating it
// changes it.
//
// Only the store's bookkeeping is left out, by name: the numbering (.seq), the format marker
// (.format), lock files (*.lock) and the temporaries of an atomic write (.tmp-*). Opening a
// registry rewrites these, and they hold nothing an answer reads. Every other file counts, dot-named
// ones too: a store reads a claim file whatever its name. A symlink is followed, to a file or a
// directory (a linked subregistry), since the store reads through it.
func Stamp(inputs []Input) (string, error) {
	h := sha256.New()
	for _, in := range inputs {
		for _, dir := range []string{in.PlanktonDir, in.NektonDir} {
			fmt.Fprintf(h, "dir %s\n", dir)
			err := stampDir(h, dir, "", map[string]bool{})
			if os.IsNotExist(err) {
				fmt.Fprintln(h, "absent")
				continue
			}
			if err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stampDir writes rel's entries under dir, in name order, following symlinks. seen holds the
// directories already on the path, so a link back up the tree ends instead of looping.
func stampDir(w io.Writer, dir, rel string, seen map[string]bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(dir, rel))
	if err != nil {
		return err
	}
	if seen[real] {
		fmt.Fprintf(w, "%s loop\n", filepath.ToSlash(rel))
		return nil
	}
	seen[real] = true
	defer delete(seen, real)
	entries, err := os.ReadDir(filepath.Join(dir, rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if bookkeeping(name) {
			continue
		}
		r := filepath.Join(rel, name)
		fi, err := os.Stat(filepath.Join(dir, r)) // follows a symlink
		if os.IsNotExist(err) {
			continue // a dangling link, or a file renamed away while we read the directory
		}
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if err := stampDir(w, dir, r, seen); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		fmt.Fprintf(w, "%s %d %d\n", filepath.ToSlash(r), fi.Size(), fi.ModTime().UnixNano())
	}
	return nil
}

func bookkeeping(name string) bool {
	return name == ".seq" || name == ".format" || strings.HasSuffix(name, ".lock") || strings.HasPrefix(name, ".tmp-")
}
