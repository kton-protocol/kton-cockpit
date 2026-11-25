// Package dirfiles lists a directory's entries by name: what filepath.Glob(filepath.Join(dir, "x*y"))
// was used for, without reading dir itself as a pattern. A directory named with *, ? or [ would
// otherwise match other directories (a repository at .../repo* reading .../repoEVIL's trusted keys),
// and one with a backslash would match nothing (kton-review G1 on #254).
package dirfiles

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Named returns the paths of dir's entries whose name starts with prefix and ends with suffix, sorted;
// none when dir does not exist. Only dir is read; nothing in its path or in prefix and suffix is a pattern.
func Named(dir, prefix, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if len(n) >= len(prefix)+len(suffix) && strings.HasPrefix(n, prefix) && strings.HasSuffix(n, suffix) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out, nil
}
