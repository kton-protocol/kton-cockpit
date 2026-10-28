package cockpit

import (
	"context"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// Open returns a Cockpit for a process that answers many calls, such as a local service (mignon's
// handoff 2.1, which cockpit-runtime serves). It answers exactly as one from New does and still
// re-reads the configuration and re-checks the binding on every call (SPEC §5). What it adds is the
// opened union, kept between calls for as long as none of its registries changes: the memory index
// (ADR-012), whatever index.kind the repository names. Close lets it go.
func Open(ctx context.Context, s Start) (*Cockpit, error) {
	c := &Cockpit{start: s, held: true}
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	c.root = cfg.RepoRoot
	return c, nil
}

// Close drops the union an Open Cockpit holds. A Cockpit from New holds nothing.
func (c *Cockpit) Close() error {
	if c != nil && c.held {
		index.Forget(c.root)
	}
	return nil
}

// reader is the read side for one call: the memory index at least when the Cockpit is held.
func (c *Cockpit) reader(cfg *config.Config) index.Reader {
	if c != nil && c.held {
		return index.OpenHeld(cfg)
	}
	return index.Open(cfg)
}
