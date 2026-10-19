package github

import (
	"context"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/gitrepo"
)

// Root is the git working tree's top level.
func (Backend) Root(ctx context.Context, dir string) (string, error) { return gitrepo.Root(ctx, dir) }

// Private is git's own directory.
func (Backend) Private() []string { return []string{".git"} }

// Head is the commit HEAD names.
func (Backend) Head(ctx context.Context, r backend.Repo) (backend.Revision, error) {
	sha, err := gitrepo.Head(ctx, r.Dir)
	if err != nil {
		return backend.Revision{}, fmt.Errorf("reading HEAD in %s: %w", r.Dir, err)
	}
	return backend.Revision{ID: sha}, nil
}

// Sync is a fast-forward pull from origin. A diverged history is refused, never merged: what to
// keep is the operator's decision, and a merge commit made on their behalf would be a write they
// did not ask for.
func (b Backend) Sync(ctx context.Context, r backend.Repo) (backend.Revision, error) {
	if err := gitrepo.PullFFOnly(ctx, r.Dir); err != nil {
		return backend.Revision{}, err
	}
	return b.Head(ctx, r)
}

// Status is what git holds of the paths, compared with the working tree.
func (Backend) Status(ctx context.Context, r backend.Repo, paths []string) (map[string]backend.State, error) {
	tracked, changed, err := gitrepo.Tracked(ctx, r.Dir, paths)
	if err != nil {
		return nil, err
	}
	out := map[string]backend.State{}
	for _, p := range paths {
		switch {
		case changed[p]:
			out[p] = backend.Changed
		case tracked[p]:
			out[p] = backend.Durable
		default:
			out[p] = backend.Untracked
		}
	}
	return out, nil
}
