// Package gitops wraps the git plumbing the cockpit performs on Claude's behalf: committing and
// pushing, and building the commit-pinned raw.githubusercontent.com permalinks every foton needs
// so peers can fetch and re-hash its bytes. It shells out to the git binary only — no
// reimplemented git logic.
package gitops

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// CommitAndPush stages exactly the given repo-relative paths, commits with message, and pushes.
// It is a no-op (not an error) if there is nothing staged to commit — repeated publishes of
// already-committed bytes should not fail.
func CommitAndPush(ctx context.Context, cfg *config.Config, paths []string, message string) (sha string, err error) {
	args := append([]string{"add"}, paths...)
	if _, err := run(ctx, cfg.RepoRoot, "git", args...); err != nil {
		return "", err
	}

	if _, err := run(ctx, cfg.RepoRoot, "git", "diff", "--cached", "--quiet"); err == nil {
		// Nothing staged — still return the current HEAD sha so callers can build permalinks.
		return CurrentSHA(ctx, cfg)
	}

	if _, err := run(ctx, cfg.RepoRoot, "git", "commit", "-m", message); err != nil {
		return "", err
	}
	if _, err := run(ctx, cfg.RepoRoot, "git", "push"); err != nil {
		return "", err
	}
	return CurrentSHA(ctx, cfg)
}

// CurrentSHA returns the repo's current HEAD commit sha.
func CurrentSHA(ctx context.Context, cfg *config.Config) (string, error) {
	out, err := run(ctx, cfg.RepoRoot, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// PermalinkBase builds the commit-pinned raw.githubusercontent.com base URL for the configured
// repo at the given sha.
func PermalinkBase(cfg *config.Config, sha string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name, sha)
}

// LocatedFlags builds one `path=permalink` pair per repo-relative path, in the form `plankton
// author --located` expects. Claude supplies plain paths; the cockpit is the only thing that ever
// constructs the URL.
func LocatedFlags(cfg *config.Config, sha string, paths []string) []string {
	base := PermalinkBase(cfg, sha)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel := filepath.ToSlash(p)
		out = append(out, fmt.Sprintf("%s=%s/%s", rel, base, rel))
	}
	return out
}
