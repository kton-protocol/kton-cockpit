package source

// git is a source read from a git repository — an aggregation repository that mirrors several
// participants, or one participant's own. It is cloned once into the source's cache, outside the
// repository, and read from there: an ask does not go to the network. `cockpit federation pull`
// brings the clone up to date, so what is read changes only when someone says so.
//
//	{"name": "hub", "kind": "git", "url": "https://github.com/org/aggregate.git"}
//	{"name": "hub", "kind": "git", "url": "…", "path": "federation"}   a layout under a subdirectory
//
// Where the registries are is found, not configured: the layouts in use are a participant's
// registry/plankton + registry/nekton, and an aggregator's plankton-data + nekton-data (at the root
// or under federation/). "path" narrows the search to one subdirectory.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type gitKind struct{}

type gitBlock struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// layouts are the registry pairs a repository may hold, relative to its root (or to path).
var layouts = [][2]string{
	{"registry/plankton", "registry/nekton"},
	{"plankton-data", "nekton-data"},
	{"federation/plankton-data", "federation/nekton-data"},
	{"federation/registry/plankton", "federation/registry/nekton"},
}

func (gitKind) Validate(s Spec) error {
	var b gitBlock
	if err := s.Decode(&b); err != nil {
		return err
	}
	if b.URL == "" {
		return fmt.Errorf("kind git needs \"url\" (the repository to read)")
	}
	if strings.Contains(b.Path, "..") {
		return fmt.Errorf("kind git: \"path\" must stay inside the repository")
	}
	return nil
}

func (gitKind) Fetch(ctx context.Context, s Spec, _, cache string) ([]Registry, error) {
	var b gitBlock
	if err := s.Decode(&b); err != nil {
		return nil, err
	}
	clone := filepath.Join(cache, "clone")
	// A clone of another URL is not this source: the cache outlives configurations (it is keyed
	// by the repository's path), so a source of the same name may have pointed elsewhere.
	if origin(ctx, clone) != b.URL {
		if err := Clone(ctx, b.URL, clone); err != nil {
			return nil, err
		}
	}
	root := filepath.Join(clone, filepath.FromSlash(b.Path))
	var out []Registry
	for _, l := range layouts {
		p, n := filepath.Join(root, l[0]), filepath.Join(root, l[1])
		_, pe := os.Stat(p)
		_, ne := os.Stat(n)
		if pe == nil || ne == nil {
			out = append(out, Registry{PlanktonDir: p, NektonDir: n})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s holds no registry (looked for %s)", b.URL, layoutNames())
	}
	return out, nil
}

// Clone makes a fresh clone of url at dir.
func Clone(ctx context.Context, url, dir string) error {
	os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", url, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("git clone %s: %s", url, strings.TrimSpace(string(out)))
	}
	return nil
}

// Pull brings a source's clone to its remote's current head and returns that commit.
func Pull(ctx context.Context, url, cache string) (string, error) {
	clone := filepath.Join(cache, "clone")
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		if err := Clone(ctx, url, clone); err != nil {
			return "", err
		}
	} else {
		for _, args := range [][]string{{"fetch", "--quiet", "origin"}, {"reset", "--quiet", "--hard", "origin/HEAD"}} {
			if out, err := exec.CommandContext(ctx, "git", append([]string{"-C", clone}, args...)...).CombinedOutput(); err != nil {
				return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
			}
		}
	}
	return Head(ctx, cache)
}

func origin(ctx context.Context, clone string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", clone, "config", "--get", "remote.origin.url").Output() // as written, not rewritten by insteadOf
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Head is the commit a source's clone is at, "" when it has none yet.
func Head(ctx context.Context, cache string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", filepath.Join(cache, "clone"), "rev-parse", "HEAD").Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func layoutNames() string {
	var n []string
	for _, l := range layouts {
		n = append(n, l[0]+" + "+l[1])
	}
	return strings.Join(n, ", ")
}

func init() { Register("git", gitKind{}) }
