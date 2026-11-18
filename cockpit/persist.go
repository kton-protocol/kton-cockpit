package cockpit

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// persist makes paths durable through the backend repo.mode selected (ADR-008). The verbs do not
// know whether that is a commit and a push, a check-in, or nothing at all.
//
// Nothing under .kton/local/ is ever persisted, by any backend: it is this checkout's own (identity,
// tool-shim streams that may hold licensed outputs, the jobs journal), and the refusal does not rest
// on .gitignore (ADR-016, kton-review L1). Nor is .kton/ as a whole, which would include it.
func persist(ctx context.Context, cfg *config.Config, paths []string, message string) (backend.Revision, error) {
	for _, p := range paths {
		if err := shareable(p); err != nil {
			return backend.Revision{}, err
		}
	}
	return cfg.Backend.Persist(ctx, cfg.Repo, paths, message)
}

// shareable refuses a repo-relative path that is, or contains, .kton/local/.
func shareable(p string) error {
	c := strings.ToLower(path.Clean(filepath.ToSlash(p)))
	local := strings.ToLower(config.Rel("local"))
	if c == "." || c == strings.ToLower(config.Dir) || c == local || strings.HasPrefix(c, local+"/") {
		return fmt.Errorf("%s is never persisted: %s/ is this checkout's own and never shared", p, local)
	}
	return nil
}

// locators are the "path=uri" pairs for a revision, in the form the kernel's author step takes.
// A backend that has no locator for a path gives none, and the record carries none for it.
func locators(cfg *config.Config, rev backend.Revision, paths []string) []string {
	var out []string
	for _, p := range paths {
		if uri, ok := cfg.Backend.Locate(cfg.Repo, rev, p); ok {
			out = append(out, p+"="+uri)
		}
	}
	return out
}

// persistStrict is persist for the writes that have always treated a rejected push as a failure:
// a claim or a scope that did not reach the shared place is reported, as it was before backends.
// Only publish carries on, because it has a record and possibly a container run to keep.
func persistStrict(ctx context.Context, cfg *config.Config, paths []string, message string) (backend.Revision, error) {
	rev, err := persist(ctx, cfg, paths, message)
	if err == nil && rev.Rejected {
		short := rev.ID
		if len(short) > 8 {
			short = short[:8]
		}
		return rev, fmt.Errorf("committed as %s, but the push was rejected: %s", short, rev.Reason)
	}
	return rev, err
}

// PersistStrict is for the operator commands (cmd/cockpit scope): the same durability rule the verbs
// apply to claims, through the same backend.
func PersistStrict(ctx context.Context, cfg *config.Config, paths []string, message string) (backend.Revision, error) {
	return persistStrict(ctx, cfg, paths, message)
}
