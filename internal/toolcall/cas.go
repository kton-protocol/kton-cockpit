package toolcall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CAS keeps the files of tool calls by their content: <Dir>/sha256/<first two>/<hash>. A
// reproduction gets the inputs as they were then, not as they are now, and a call whose key
// has been seen before gets its outputs from here. Only inputs and outputs of calls are kept:
// what no call read and no call wrote is not needed to compute anything again.
type CAS struct {
	Dir     string
	MaxSize int64 // larger files are not kept (0: no limit)
}

// Path is where a hash is kept, whether or not it is.
func (c *CAS) Path(sum string) string {
	sum = strings.TrimPrefix(sum, "sha256:")
	if len(sum) < 3 {
		return filepath.Join(c.Dir, "sha256", "_", sum)
	}
	return filepath.Join(c.Dir, "sha256", sum[:2], sum)
}

// Has reports whether the bytes of this hash are kept.
func (c *CAS) Has(sum string) bool {
	_, err := os.Stat(c.Path(sum))
	return err == nil
}

// Put keeps the file at path under sum if its bytes still hash to sum. It reports whether the
// bytes are kept afterwards; a file that changed or is gone is not an error, only not kept.
func (c *CAS) Put(path, sum string) (bool, error) {
	sum = strings.TrimPrefix(sum, "sha256:")
	if len(sum) != 64 {
		return false, nil
	}
	if c.Has(sum) {
		return true, nil
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false, nil
	}
	if c.MaxSize > 0 && fi.Size() > c.MaxSize {
		return false, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return false, nil
	}
	defer in.Close()
	dst := c.Path(sum)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".put-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		tmp.Close()
		return false, fmt.Errorf("copy %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return false, nil // changed since it was observed: these bytes are gone
	}
	os.Chmod(tmp.Name(), 0o444)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return false, err
	}
	return true, nil
}
