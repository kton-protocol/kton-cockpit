// Package github is the cockpit's git backend, the reference implementation of cockpit/backend
// (ADR-008): a git working tree whose origin is a GitHub repository.
//
//   - Bind: the configuration sits at the repository root, and git's own origin remote names the
//     owner and repository the configuration claims (SPEC §5.2). These are two independent sources.
//   - Persist: commit, then push to origin (internal/gitops).
//   - Locate: the commit-pinned raw.githubusercontent.com URL.
//
// Importing the package registers it as mode "git", which is also the default.
package github

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/gitops"
	"github.com/kton-protocol/kton-cockpit/internal/gitrepo"
)

// Mode is the repo.mode this backend serves.
const Mode = "git"

func init() { backend.Register(Mode, Backend{}) }

// Config is the repo block in git mode.
type Config struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// Backend implements backend.Backend for git on GitHub.
type Backend struct{}

func (Backend) Validate(r backend.Repo) error {
	var c Config
	if err := r.Decode(&c); err != nil {
		return err
	}
	var missing []string
	if c.Owner == "" {
		missing = append(missing, "repo.owner")
	}
	if c.Name == "" {
		missing = append(missing, "repo.name")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func (Backend) Bind(ctx context.Context, r backend.Repo) error {
	var c Config
	if err := r.Decode(&c); err != nil {
		return err
	}
	repoRoot, err := gitrepo.Root(ctx, r.Dir)
	if err != nil {
		return fmt.Errorf(
			"not inside a git repository (%s), and repo.mode is not %q: %w", r.Dir, "local", err)
	}
	if !gitrepo.SamePath(repoRoot, r.Dir) {
		return fmt.Errorf(
			"cockpit.config.json is at %s but the git repository root is %s — the config must sit at the "+
				"repository root, so that what it binds is unambiguous", r.Dir, repoRoot)
	}
	return checkRemoteMatches(ctx, repoRoot, c)
}

// checkRemoteMatches is the hard-refuse step of the anti-wrong-folder guard: the repo's own
// `origin` remote must name the exact owner/repo the config claims to be. On any mismatch —
// including no remote at all — every cockpit tool call refuses outright.
func checkRemoteMatches(ctx context.Context, repoRoot string, want Config) error {
	url, err := gitrepo.OriginURL(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("could not read git remote 'origin' in %s: %w", repoRoot, err)
	}
	owner, name, ok := gitrepo.GitHubOwnerRepo(url)
	if !ok {
		return fmt.Errorf("remote 'origin' (%s) is not a recognizable github.com owner/repo URL", url)
	}
	if !strings.EqualFold(owner, want.Owner) || !strings.EqualFold(name, want.Name) {
		return fmt.Errorf(
			"repo/remote mismatch: cockpit.config.json says %s/%s, but this repo's origin is %s/%s — refusing to act against the wrong repo",
			want.Owner, want.Name, owner, name,
		)
	}
	return nil
}

func (Backend) Persist(ctx context.Context, r backend.Repo, paths []string, message string) (backend.Revision, error) {
	sha, err := gitops.CommitAndPush(ctx, gitops.Git{Root: r.Dir, Commit: r.Commit, Push: r.Push,
		SessionID: r.SessionID}, paths, message)
	if err != nil {
		// A rejected push is not a failed write: the commit is made, its sha is real, and the
		// permalinks built from it resolve the moment somebody pushes.
		var pf *gitops.PushFailed
		if asPushFailed(err, &pf) {
			return backend.Revision{ID: pf.SHA, Rejected: true, Reason: pf.Err.Error()}, nil
		}
		return backend.Revision{}, err
	}
	return backend.Revision{ID: sha}, nil
}

// Locate pins the path to the commit. No commit, no locator: a URL pinned to no commit, or to
// whatever HEAD happened to be, points at bytes that are not there.
func (Backend) Locate(r backend.Repo, rev backend.Revision, path string) (string, bool) {
	if rev.ID == "" {
		return "", false
	}
	var c Config
	if err := r.Decode(&c); err != nil {
		return "", false
	}
	return gitops.PermalinkBase(c.Owner, c.Name, rev.ID) + "/" + filepath.ToSlash(path), true
}
