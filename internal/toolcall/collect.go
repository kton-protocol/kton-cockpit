package toolcall

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CheckRel accepts only a clean relative path: no leading slash, no "..", ".", empty segment or
// NUL, nothing a staging directory could be left through.
func CheckRel(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("an empty path")
	case strings.ContainsRune(p, 0):
		return fmt.Errorf("%q contains a NUL byte", p)
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p):
		return fmt.Errorf("%q is absolute; a call names files relative to the repository root", p)
	case strings.Contains(p, "\\"):
		return fmt.Errorf("%q contains a backslash", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%q has a %q segment; a call names files inside the repository only", p, seg)
		}
	}
	return nil
}

// Collect finds a call's inputs, as paths relative to root, the repository root: what collector
// says the call reads, given its arguments as NormArgv leaves them (relative to cwd, itself relative
// to root). A file the call names outside the repository is an error: it would not exist where the
// call runs, and leaving it out would make a wrong record (ADR-014, section 2).
func Collect(collector, root, cwd string, args []string) ([]string, error) {
	c := collection{root: root, seen: map[string]bool{}}
	switch collector {
	case "files":
		for _, a := range args {
			if _, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") {
				a = v
			}
			if err := c.add(cwd, a, false); err != nil {
				return nil, err
			}
		}
	case "nonmem":
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				if err := c.controlStream(cwd, a); err != nil {
					return nil, err
				}
				break // the first argument is the control stream; the second is the listing it writes
			}
		}
	default:
		return nil, fmt.Errorf("no collector %q", collector)
	}
	return c.out, nil
}

type collection struct {
	root string
	seen map[string]bool
	out  []string
}

// add takes p, relative to the directory dir (relative to the root), when it names a regular file
// in the repository. With must, a file that is not there is an error, not something to skip.
func (c *collection) add(dir, p string, must bool) error {
	if p == "" {
		return nil
	}
	if filepath.IsAbs(p) {
		if must {
			return fmt.Errorf("%s is outside the repository: the call would not find it where it runs", p)
		}
		return nil
	}
	rel := path.Clean(path.Join(dir, filepath.ToSlash(p)))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%s (from %s) is outside the repository: the call would not find it where it runs", p, dir)
	}
	fi, err := os.Lstat(filepath.Join(c.root, filepath.FromSlash(rel)))
	switch {
	case err != nil && must:
		return fmt.Errorf("%s names %s, which is not there", dir, rel)
	case err != nil || !fi.Mode().IsRegular():
		return nil
	}
	if !c.seen[rel] {
		c.seen[rel] = true
		c.out = append(c.out, rel)
	}
	return nil
}

// controlStream takes a NONMEM control stream and the files it reads: $DATA, $MSFI and, followed
// into, $INCLUDE. NONMEM resolves them relative to the run's directory.
func (c *collection) controlStream(dir, p string) error {
	if err := c.add(dir, p, true); err != nil {
		return err
	}
	rel := path.Clean(path.Join(dir, p))
	b, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, ";"); i >= 0 {
			line = line[:i] // a comment
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch rec := strings.ToUpper(f[0]); {
		case strings.HasPrefix(rec, "$DAT"), strings.HasPrefix(rec, "$MSFI"):
			if err := c.add(dir, unquote(f[1]), true); err != nil {
				return err
			}
		case strings.HasPrefix(rec, "$INCLUDE"):
			if err := c.controlStream(dir, unquote(f[1])); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func unquote(s string) string { return strings.Trim(s, `"'`) }
