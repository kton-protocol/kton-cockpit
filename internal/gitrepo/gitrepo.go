// Package gitrepo asks git the few questions the cockpit needs about a working tree: where its root
// is, what its origin says, and whether two paths are the same directory. It is shared by the
// configuration search (SPEC §5.1), the git and local backends, and the operator commands, which
// each used to carry their own copy.
package gitrepo

import (
	"context"
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
