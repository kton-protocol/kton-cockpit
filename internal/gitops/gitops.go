// Package gitops wraps the git plumbing the cockpit performs on Claude's behalf: committing and
// pushing, and building the commit-pinned raw.githubusercontent.com permalinks every foton needs
// so peers can fetch and re-hash its bytes. It shells out to the git binary only — no
// reimplemented git logic.
package gitops

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
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

// runRedacted behaves like run, but scrubs secret from both the command line and the command's
// output before it can appear in a returned error — used for git push, whose auth header must
// never leak into an error message that becomes visible to Claude or a log file.
func runRedacted(ctx context.Context, dir, name string, args []string, secret string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		safeArgs := args
		outStr := string(out)
		if secret != "" {
			safeArgs = redactSlice(args, secret)
			outStr = strings.ReplaceAll(outStr, secret, "[REDACTED]")
		}
		return outStr, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(safeArgs, " "), err, outStr)
	}
	return string(out), nil
}

func redactSlice(args []string, secret string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, secret, "[REDACTED]")
	}
	return out
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

	// The commit identity is passed via -c, not written to any git config file: it must not
	// depend on the ambient environment already having user.name/user.email set (it often
	// doesn't, e.g. a fresh claude-science sandbox), and -c sidesteps a real failure mode seen in
	// practice where writing to .git/config directly hit "Device or resource busy" inside that
	// sandbox. Derived from the session identity already in config — not a new config field.
	commitArgs := append(commitIdentityArgs(cfg), "commit", "-m", message)
	if _, err := run(ctx, cfg.RepoRoot, "git", commitArgs...); err != nil {
		return "", err
	}
	if err := push(ctx, cfg); err != nil {
		return "", err
	}
	return CurrentSHA(ctx, cfg)
}

// commitIdentityArgs returns the `-c user.name=... -c user.email=...` flags for this cockpit's
// own commits, derived from the configured session identity so every participant's commits are
// attributable without requiring git to be pre-configured in the environment.
func commitIdentityArgs(cfg *config.Config) []string {
	session := cfg.Raw.Identity.SessionID
	return []string{
		"-c", fmt.Sprintf("user.name=cockpit (%s)", session),
		"-c", fmt.Sprintf("user.email=%s@cockpit.local", session),
	}
}

// push runs git push. If GITHUB_TOKEN or GH_TOKEN is set in the cockpit's own process
// environment, it authenticates with that token via an HTTP auth header passed through -c —
// never by rewriting the remote URL (which would persist the token into .git/config) and never
// logged unredacted on failure. This is opt-in and additive: with neither variable set, push
// behaves exactly as before, relying on whatever git credential setup already exists in the
// environment (a normal terminal with `gh auth login` already configured, for instance). It
// exists because a sandboxed MCP client runtime (claude-science's Local command connector) does
// not necessarily inherit that ambient credential setup, so git push there fails with no way to
// authenticate unless told to explicitly.
func push(ctx context.Context, cfg *config.Config) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		_, err := run(ctx, cfg.RepoRoot, "git", "push")
		return err
	}
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	args := []string{"-c", "http.extraheader=" + header, "push"}
	_, err := runRedacted(ctx, cfg.RepoRoot, "git", args, token)
	return err
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
