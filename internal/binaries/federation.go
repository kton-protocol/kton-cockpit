package binaries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	nregistry "kton.dev/nekton/registry"
	pregistry "kton.dev/plankton/registry"
)

// ReadDirs are the registries a READ goes to: this repository's own, or — when the configuration
// names federated sources — the union of it and every source, joined by record id.
//
// Writes never come here. A publish, a claim, a seal goes to the own registry, and the union is
// rebuilt from it on the next read, so a record written a moment ago is never missing from what is
// read next. Every record entering the union passes the registry's own checks on Add; a source that
// holds one that does not is named, not skipped — a partial union must not pass for a whole one.
// Trust does not widen: the configured tiers stay the ceiling for every answer.
func ReadDirs(ctx context.Context, cfg *config.Config) (planktonDir, nektonDir string, err error) {
	srcs := cfg.Raw.Federation.Sources
	if len(srcs) == 0 {
		return cfg.PlanktonDir, cfg.NektonDir, nil
	}
	base, err := cacheDir(cfg)
	if err != nil {
		return "", "", err
	}
	union := filepath.Join(base, "union")
	if err := os.RemoveAll(union); err != nil {
		return "", "", err
	}
	pdir, ndir := filepath.Join(union, "plankton"), filepath.Join(union, "nekton")
	preg, err := pregistry.Open(pdir)
	if err != nil {
		return "", "", err
	}
	nreg, err := nregistry.Open(ndir)
	if err != nil {
		return "", "", err
	}
	add := func(name string, reg source.Registry) error {
		if p, err := pregistry.Open(reg.PlanktonDir); err == nil {
			if n := p.Degraded(); n > 0 {
				return fmt.Errorf("source %s: its plankton registry skipped %d record(s) on load", name, n)
			}
			for _, rec := range p.Records(0) {
				if _, _, err := preg.Add(rec.Envelope); err != nil {
					return fmt.Errorf("source %s: plankton %s: %w", name, rec.FotonID, err)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("source %s: %w", name, err)
		}
		if n, err := nregistry.Open(reg.NektonDir); err == nil {
			for _, rec := range n.Records(0) {
				if _, _, err := nreg.Add(rec.Envelope); err != nil {
					return fmt.Errorf("source %s: nekton %s: %w", name, rec.ClaimID, err)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("source %s: %w", name, err)
		}
		return nil
	}
	if err := add("(own)", source.Registry{PlanktonDir: cfg.PlanktonDir, NektonDir: cfg.NektonDir}); err != nil {
		return "", "", err
	}
	for _, s := range srcs {
		k, _ := source.Lookup(s.Kind) // validated at load
		cache := filepath.Join(base, "sources", s.Name)
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return "", "", err
		}
		regs, err := k.Fetch(ctx, s, cfg.RepoRoot, cache)
		if err != nil {
			return "", "", fmt.Errorf("federation source %s (%s): %w", s.Name, s.Kind, err)
		}
		for _, reg := range regs {
			if err := add(s.Name, reg); err != nil {
				return "", "", err
			}
		}
	}
	return pdir, ndir, nil
}

// cacheDir is outside the repository: the union is derived, and a repository holds nothing the
// cockpit derived on its own.
func cacheDir(cfg *config.Config) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(cfg.RepoRoot))
	d := filepath.Join(root, "kton-cockpit", hex.EncodeToString(h[:8]))
	return d, os.MkdirAll(d, 0o755)
}

// Union is the read side, opened: the registries ReadDirs names, as the kernel loaded them. Reads
// only — a write goes to the own registry, never to a union.
type Union struct {
	Plankton *pregistry.Registry
	Nekton   *nregistry.Registry
}

// An Opener opens the union a configuration reads from. Fresh opens it from the registries every
// time; a derived index (internal/index) keeps what it opened as long as nothing it was opened from
// has changed. Every Opener must answer as Fresh would.
type Opener interface {
	Open(ctx context.Context, cfg *config.Config) (*Union, error)
}

// OpenerFunc is a function as an Opener.
type OpenerFunc func(ctx context.Context, cfg *config.Config) (*Union, error)

func (f OpenerFunc) Open(ctx context.Context, cfg *config.Config) (*Union, error) { return f(ctx, cfg) }

// Fresh opens the union from the registries, holding nothing afterwards.
var Fresh Opener = OpenerFunc(OpenUnion)

// OpenUnion opens what ReadDirs names.
func OpenUnion(ctx context.Context, cfg *config.Config) (*Union, error) {
	pdir, ndir, err := ReadDirs(ctx, cfg)
	if err != nil {
		return nil, err
	}
	preg, err := pregistry.Open(pdir)
	if err != nil {
		return nil, fmt.Errorf("opening the plankton registry: %w", err)
	}
	nreg, err := nregistry.Open(ndir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	return &Union{Plankton: preg, Nekton: nreg}, nil
}

type heldUnion struct {
	u       *Union
	sources string // the federation sources it was opened from
}

func (r *Runner) read(ctx context.Context) (*Union, error) {
	srcs, _ := json.Marshal(r.cfg.Raw.Federation.Sources)
	if r.held != nil && r.held.sources == string(srcs) {
		return r.held.u, nil
	}
	u, err := r.opener.Open(ctx, r.cfg)
	if err != nil {
		return nil, err
	}
	r.held = &heldUnion{u, string(srcs)}
	return u, nil
}

// wrote drops the union after a write to the own registry: the next read opens it again.
func (r *Runner) wrote() { r.held = nil }
