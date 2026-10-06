// Package local is the cockpit's backend for a plain directory with no git repository (ADR-004),
// part of the reference implementation of cockpit/backend (ADR-008).
//
//   - Bind: the configuration declares its own absolute path, and it must be where it actually is
//     (SPEC §5.3). Local mode is refused inside a git repository that has an origin, because there
//     the remote is the stronger second source.
//   - Persist: nothing; there is nothing to commit to.
//   - Locate: none; with no commit there is nothing to pin a locator to.
//
// Importing the package registers it as mode "local".
package local

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/gitrepo"
)

// Mode is the repo.mode this backend serves.
const Mode = "local"

func init() { backend.Register(Mode, Backend{}) }

// Config is the repo block in local mode.
type Config struct {
	Root string `json:"root"`
}

// Backend implements backend.Backend for a plain directory.
type Backend struct{}

func (Backend) Validate(r backend.Repo) error {
	var c Config
	if err := r.Decode(&c); err != nil {
		return err
	}
	if c.Root == "" {
		return fmt.Errorf("missing required field(s): repo.root (the absolute path this config belongs to — in local mode it is what the guard checks, since there is no remote to disagree with)")
	}
	if !filepath.IsAbs(c.Root) {
		return fmt.Errorf("repo.root must be an absolute path, got %q", c.Root)
	}
	return nil
}

func (Backend) Bind(ctx context.Context, r backend.Repo) error {
	var c Config
	if err := r.Decode(&c); err != nil {
		return err
	}
	// Accepting local mode inside a repository that HAS an origin would silently trade the stronger
	// check for the weaker: the remote is an independent second source, and the declared path is
	// only the config agreeing with itself. A config that turns that off by naming a mode, in a repo
	// where it was available, is the kind of quiet downgrade this guard exists to prevent.
	if root, err := gitrepo.Root(ctx, r.Dir); err == nil {
		if _, rerr := gitrepo.OriginURL(ctx, root); rerr == nil {
			return fmt.Errorf(
				"repo.mode is %q, but %s is a git repository with an origin remote — local mode would "+
					"drop the remote check for a weaker one. Use the default git mode here",
				Mode, root)
		}
	}
	declared, err := filepath.Abs(c.Root)
	if err != nil {
		return fmt.Errorf("repo.root %q is not a usable path: %w", c.Root, err)
	}
	if !gitrepo.SamePath(declared, r.Dir) {
		return fmt.Errorf(
			"location mismatch: cockpit.config.json says it belongs at %s, but it is at %s — refusing "+
				"to act against a directory this config was not written for", declared, r.Dir)
	}
	return nil
}

func (Backend) Persist(context.Context, backend.Repo, []string, string) (backend.Revision, error) {
	return backend.Revision{}, nil
}

func (Backend) Locate(backend.Repo, backend.Revision, string) (string, bool) { return "", false }
