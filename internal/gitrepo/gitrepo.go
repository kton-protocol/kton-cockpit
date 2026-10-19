// Package gitrepo asks git the few questions the cockpit needs about a working tree: where its root
// is, what its origin says, and whether two paths are the same directory. It is shared by the
// configuration search (SPEC §5.1), the git and local backends, and the operator commands, which
// each used to carry their own copy.
package gitrepo

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Root is the root of the git repository containing dir.
func Root(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// OriginURL reads a repository's origin remote, or errors if it has none.
func OriginURL(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var githubRe = regexp.MustCompile(`(?:github\.com[:/])([^/]+)/([^/.]+?)(?:\.git)?$`)

// GitHubOwnerRepo parses a github.com remote URL into owner and repository name.
func GitHubOwnerRepo(url string) (owner, name string, ok bool) {
	m := githubRe.FindStringSubmatch(url)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// SamePath reports whether two paths name the same directory, following symlinks.
func SamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// Head is the commit HEAD names.
func Head(ctx context.Context, root string) (string, error) {
	return output(ctx, root, "rev-parse", "HEAD")
}

// PullFFOnly takes in origin's commits when that needs no merge, and refuses otherwise.
func PullFFOnly(ctx context.Context, root string) error {
	cmd := exec.CommandContext(ctx, "git", "pull", "--ff-only", "--quiet")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull --ff-only: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Tracked lists which of paths git holds, and which of those differ from the working tree.
func Tracked(ctx context.Context, root string, paths []string) (tracked, changed map[string]bool, err error) {
	tracked, changed = map[string]bool{}, map[string]bool{}
	ls, err := output(ctx, root, append([]string{"ls-files", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range strings.Split(ls, "\x00") {
		if p != "" {
			tracked[p] = true
		}
	}
	diff, err := output(ctx, root, append([]string{"diff", "--name-only", "-z", "HEAD", "--"}, paths...)...)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range strings.Split(diff, "\x00") {
		if p != "" {
			changed[p] = true
		}
	}
	return tracked, changed, nil
}

func output(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
