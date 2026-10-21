package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/gitops"
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

// Sync pulls from origin — a fast-forward, or a merge commit when both sides moved, never a rebase:
// the local commits are pinned in the locators of records already signed, and a rebase would leave
// those pointing at nothing. A conflict aborts the merge and refuses. Then, when sharing is on, it
// pushes; a push origin refuses is reported as Rejected, not as an error.
func (b Backend) Sync(ctx context.Context, r backend.Repo) (backend.Revision, error) {
	// The merge commit is the cockpit's, made under the session's identity as its other commits are.
	if err := gitrepo.PullMerge(ctx, r.Dir, gitops.IdentityArgs(r.SessionID)...); err != nil {
		return backend.Revision{}, err
	}
	head, err := b.Head(ctx, r)
	if err != nil || !r.Share {
		return head, err
	}
	if err := gitrepo.Push(ctx, r.Dir); err != nil {
		head.Rejected, head.Reason = true, err.Error()
	}
	return head, nil
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

// Propose binds the working tree containing dir to the GitHub repository its origin names. Without
// an origin the owner and name stay empty, and doctor asks for them.
func (Backend) Propose(ctx context.Context, dir string) (backend.Proposal, bool) {
	root, err := gitrepo.Root(ctx, dir)
	if err != nil {
		return backend.Proposal{}, false
	}
	var c Config
	if url, err := gitrepo.OriginURL(ctx, root); err == nil {
		c.Owner, c.Name, _ = gitrepo.GitHubOwnerRepo(url)
	}
	b, _ := json.Marshal(c)
	return backend.Proposal{Root: root, Block: b,
		Describe: fmt.Sprintf("git mode, owner=%q name=%q", c.Owner, c.Name)}, true
}
