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

// runRedacted behaves like run, but scrubs every given secret from both the command line and the
// command's output before it can appear in a returned error — used for git push, whose auth
// header must never leak into an error message that becomes visible to Claude or a log file.
//
// Takes secrets, plural: an independent cold-session security review found that push() passing
// only the raw token here left the actual leak vector completely unredacted. The credential never
// appears in argv as the raw token — it's embedded in a base64-encoded HTTP header
// ("AUTHORIZATION: basic <base64(x-access-token:<token>)>"), and base64 encoding does not
// preserve substrings, so a redact pass matching only the raw token string never matches anything
// in that argv element. Confirmed live: a failed push with GITHUB_TOKEN set returned the fully
// intact, trivially-decodable base64 blob straight into the MCP tool result. push() now passes
// both the raw token AND the constructed header string here, so either form is scrubbed.
func runRedacted(ctx context.Context, dir, name string, args []string, secrets ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		safeArgs := redactSlice(args, secrets...)
		outStr := string(out)
		for _, s := range secrets {
			if s != "" {
				outStr = strings.ReplaceAll(outStr, s, "[REDACTED]")
			}
		}
		return outStr, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(safeArgs, " "), err, outStr)
	}
	return string(out), nil
}

func redactSlice(args []string, secrets ...string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for i, a := range out {
			out[i] = strings.ReplaceAll(a, secret, "[REDACTED]")
		}
	}
	return out
}

// CommitAndPush stages exactly the given repo-relative paths, commits with message, and pushes.
// It is a no-op (not an error) if there is nothing staged to commit — repeated publishes of
// already-committed bytes should not fail.
func CommitAndPush(ctx context.Context, cfg *config.Config, paths []string, message string) (sha string, err error) {
	// The "--" separator is load-bearing, not cosmetic: without it, a path that happens to start
	// with "-" (e.g. "-f") is parsed by git as a FLAG, not a literal path — "git add -f ." force-
	// adds every gitignored file in the repo, keys included, without the string "keys/..." ever
	// appearing in the request. internal/tools/publish.go's validatePublishPath also rejects
	// leading-dash paths as defense in depth, but this is the actual, root-cause fix: with "--",
	// everything after it is unconditionally a pathspec, regardless of what it starts with.
	args := append([]string{"add", "--"}, paths...)
	if _, err := run(ctx, cfg.RepoRoot, "git", args...); err != nil {
		return "", err
	}

	if _, err := run(ctx, cfg.RepoRoot, "git", "diff", "--cached", "--quiet"); err == nil {
		// Nothing NEW staged this call — but HEAD may already be ahead of origin from a PRIOR
		// call whose commit succeeded and whose push then failed (network blip, auth hiccup): a
		// naive early return here, without ever attempting a push, would leave that commit
		// stranded locally forever, since every subsequent publish of the same already-committed
		// bytes takes this exact "nothing staged" branch too.
		//
		// Only push if HEAD is actually ahead of (or has diverged from) the upstream tracking
		// ref — an independent cold-session review pointed out that pushing unconditionally here
		// turns every republish of already-in-sync content into a real network round-trip, where
		// it used to be a pure local no-op; in a flaky/offline sandboxed connector runtime (the
		// kind this project's own push() already accounts for) that can hang or fail on network
		// trouble the call never needed to risk. localAheadOfUpstream errs toward pushing (returns
		// true) whenever it can't cleanly determine "definitely already in sync" — e.g. no
		// upstream tracking ref configured yet — so this never trades away the stranded-commit fix
		// above; it only skips the network call in the unambiguous already-synced case.
		ahead, aerr := localAheadOfUpstream(ctx, cfg)
		if aerr != nil {
			return "", aerr
		}
		if ahead {
			if err := push(ctx, cfg); err != nil {
				return "", err
			}
		}
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

// push runs git push, explicitly targeting "origin HEAD" rather than a bare `git push`: a bare
// push relies on ambient state this cockpit never controls or verifies — an already-configured
// upstream tracking branch, and whatever the environment's push.default happens to be (`simple`,
// `matching`, `nothing`, ...). A fresh clone with no tracking branch set makes a bare push fail
// outright ("no upstream branch"); a `matching` config could push OTHER local branches the cockpit
// never touched. "origin HEAD" pushes exactly the currently checked-out commit to the
// same-named branch on origin, unconditionally, regardless of local tracking/push.default state —
// and is a documented no-op ("Everything up-to-date") when there's nothing new to send, so it's
// also safe to call speculatively (see the "nothing staged" branch above).
//
// If GITHUB_TOKEN or GH_TOKEN is set in the cockpit's own process environment, it authenticates
// with that token via an HTTP auth header passed through -c — never by rewriting the remote URL
// (which would persist the token into .git/config) and never logged unredacted on failure. This
// is opt-in and additive: with neither variable set, push behaves exactly as before, relying on
// whatever git credential setup already exists in the environment (a normal terminal with `gh
// auth login` already configured, for instance). It exists because a sandboxed MCP client runtime
// (claude-science's Local command connector) does not necessarily inherit that ambient credential
// setup, so git push there fails with no way to authenticate unless told to explicitly.
func push(ctx context.Context, cfg *config.Config) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		_, err := run(ctx, cfg.RepoRoot, "git", "push", "origin", "HEAD")
		return err
	}
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	args := []string{"-c", "http.extraheader=" + header, "push", "origin", "HEAD"}
	// Both the raw token and the constructed header are passed to runRedacted: the header (which
	// actually appears in argv) is the real leak vector since it's base64, not the raw token
	// substring — see runRedacted's doc comment. The raw token is included too as defense in
	// depth in case it ever surfaces some other way.
	_, err := runRedacted(ctx, cfg.RepoRoot, "git", args, token, header)
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

// localAheadOfUpstream reports whether HEAD differs from the upstream tracking ref (ahead,
// behind, or diverged) — a purely local comparison against the last-known remote-tracking ref
// (`@{u}`), no network call. Returns true — "assume a push is needed" — whenever that comparison
// can't be made cleanly, e.g. no upstream tracking ref is configured yet (a fresh clone before
// its first push): this is only ever used to decide whether to SKIP a push, so erring toward true
// on ambiguity never reintroduces the stranded-commit bug CommitAndPush's "nothing staged" branch
// exists to fix — it only skips the network round-trip in the unambiguous already-synced case.
func localAheadOfUpstream(ctx context.Context, cfg *config.Config) (bool, error) {
	head, err := CurrentSHA(ctx, cfg)
	if err != nil {
		return false, err
	}
	upstream, err := run(ctx, cfg.RepoRoot, "git", "rev-parse", "@{u}")
	if err != nil {
		return true, nil
	}
	return head != strings.TrimSpace(upstream), nil
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
