package binaries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/flock"
	nregistry "kton.dev/nekton/registry"
	pregistry "kton.dev/plankton/registry"
)

// ReadDirs are the registries a READ goes to: this repository's own, or — when the configuration
// names federated sources — the union of it and every source, joined by record id.
//
// Writes never come here. A publish, a claim, a seal goes to the own registry, which changes the
// stamp the union is kept under, so the next read builds a new one and a record written a moment
// ago is never missing from what is read next. Every record entering the union passes the registry's own checks on Add; a source that
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
	// Two cockpits reading one repository — an editor and a watcher, or fifteen asks at once — take
	// turns fetching sources and building, and a union once built is never touched again: it lives
	// under the stamp of what it was built from, so a reader holding one keeps reading it while the
	// next one is built beside it.
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", err
	}
	unlock, err := flock.Lock(filepath.Join(base, "union.lock"))
	if err != nil {
		return "", "", fmt.Errorf("locking the union cache: %w", err)
	}
	defer unlock()
	inputs, err := Inputs(ctx, cfg)
	if err != nil {
		return "", "", err
	}
	stamp, err := Stamp(inputs)
	if err != nil {
		return "", "", err
	}
	union := filepath.Join(base, "union-"+stamp[:32])
	defer sweepUnions(base, union)
	if _, err := os.Stat(union); err == nil {
		now := time.Now()
		_ = os.Chtimes(union, now, now) // in use: the sweep goes by when a union was last read
		return filepath.Join(union, "plankton"), filepath.Join(union, "nekton"), nil
	}
	build, err := os.MkdirTemp(base, "union-build-")
	if err != nil {
		return "", "", err
	}
	if err := buildUnion(build, inputs); err != nil {
		os.RemoveAll(build)
		return "", "", err
	}
	if err := os.Rename(build, union); err != nil {
		os.RemoveAll(build)
		return "", "", err
	}
	return filepath.Join(union, "plankton"), filepath.Join(union, "nekton"), nil
}

// unionTTL is how long a union nobody has read is kept once it is no longer the current one.
const unionTTL = time.Hour

// sweepUnions removes unions and abandoned builds nobody has read for unionTTL, except current,
// and the single union directory older cockpits rebuilt in place.
func sweepUnions(base, current string) {
	_ = os.RemoveAll(filepath.Join(base, "union"))
	ents, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range ents {
		p := filepath.Join(base, e.Name())
		if !e.IsDir() || p == current || !strings.HasPrefix(e.Name(), "union-") {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > unionTTL {
			_ = os.RemoveAll(p)
		}
	}
}

// buildUnion joins inputs into fresh registries under dir.
func buildUnion(dir string, inputs []Input) error {
	pdir, ndir := filepath.Join(dir, "plankton"), filepath.Join(dir, "nekton")
	preg, err := pregistry.Open(pdir)
	if err != nil {
		return err
	}
	nreg, err := nregistry.Open(ndir)
	if err != nil {
		return err
	}
	add := func(name string, reg source.Registry) error {
		if p, err := OpenPlanktonSource(reg.PlanktonDir); err == nil {
			if n := p.Degraded(); n > 0 {
				return fmt.Errorf("source %s: its plankton registry skipped %d record(s) on load", name, n)
			}
			for _, rec := range p.Records(0) {
				if _, _, err := preg.Add(rec.Envelope); err != nil {
					return fmt.Errorf("source %s: plankton %s: %w", name, rec.FotonID, err)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source %s: %w", name, err)
		}
		if n, err := OpenNektonSource(reg.NektonDir); err == nil {
			for _, rec := range n.Records(0) {
				if _, _, err := nreg.Add(rec.Envelope); err != nil {
					return fmt.Errorf("source %s: nekton %s: %w", name, rec.ClaimID, err)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source %s: %w", name, err)
		}
		return nil
	}
	for _, in := range inputs {
		if err := add(in.Name, in.Registry); err != nil {
			return err
		}
	}
	return nil
}

// Input is one registry a union is built from: the own one, or one a federation source brought.
type Input struct {
	Name string // Own, or the source's name
	source.Registry
}

// Own names the repository's own registries among the Inputs.
const Own = "(own)"

// Inputs are the registries the union of cfg is built from, in the order they are joined. A source
// is fetched into its cache first, as the union does, and refused when it holds no registry at all.
// The own registry is never refused: before the first publish there is none yet.
func Inputs(ctx context.Context, cfg *config.Config) ([]Input, error) {
	out := []Input{{Own, source.Registry{PlanktonDir: cfg.PlanktonDir, NektonDir: cfg.NektonDir}}}
	srcs := cfg.Raw.Federation.Sources
	if len(srcs) == 0 {
		return out, nil
	}
	base, err := cacheDir(cfg)
	if err != nil {
		return nil, err
	}
	for _, s := range srcs {
		k, _ := source.Lookup(s.Kind) // validated at load
		cache := filepath.Join(base, "sources", s.Name)
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return nil, err
		}
		regs, err := k.Fetch(ctx, s, cfg.RepoRoot, cache)
		if err != nil {
			return nil, fmt.Errorf("federation source %s (%s): %w", s.Name, s.Kind, err)
		}
		for _, reg := range regs {
			// A source with no registry at all is a wrong path, not an empty source; every reader of
			// the union (the union itself, the index) gets the same refusal from here.
			if !Exists(reg.PlanktonDir) && !Exists(reg.NektonDir) {
				return nil, fmt.Errorf("source %s: there is no registry at %s or %s", s.Name, reg.PlanktonDir, reg.NektonDir)
			}
			out = append(out, Input{s.Name, reg})
		}
	}
	return out, nil
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
	if len(cfg.Raw.Federation.Sources) > 0 {
		// A union is shared by every reader of this repository, so it is read the way a source is:
		// opening it writably would have the kernel persist its numbering into it under the others.
		preg, err := OpenPlanktonSource(pdir)
		if err != nil {
			return nil, fmt.Errorf("opening the plankton union: %w", err)
		}
		nreg, err := OpenNektonSource(ndir)
		if err != nil {
			return nil, fmt.Errorf("opening the nekton union: %w", err)
		}
		return &Union{Plankton: preg, Nekton: nreg}, nil
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

// wrote drops the union and the own registries after a write to the own registry: the next read
// opens them again.
func (r *Runner) wrote() { r.held, r.ownP, r.ownN = nil, nil, nil }

// Reading a registry the cockpit does not own must not change it. The kernel's Open is the writer's:
// it numbers the store and persists that (.seq, a fresh epoch), and creates a directory that is not
// there, so a read would write into a federation source and a mistyped path would become an empty
// registry. A source is read with the kernel's read-only open (OpenUnion of one directory), which
// numbers in memory only and never creates anything. (kton-review W1 on PR #10.)

// OpenPlanktonSource opens a plankton registry read-only. A directory that is not there is
// os.ErrNotExist: an empty layer, not one to create.
func OpenPlanktonSource(dir string) (*pregistry.Registry, error) {
	if !Exists(dir) {
		return nil, fmt.Errorf("%s: %w", dir, os.ErrNotExist)
	}
	return pregistry.OpenUnion(dir)
}

// OpenNektonSource opens a nekton registry read-only, as OpenPlanktonSource does.
func OpenNektonSource(dir string) (*nregistry.Registry, error) {
	if !Exists(dir) {
		return nil, fmt.Errorf("%s: %w", dir, os.ErrNotExist)
	}
	return nregistry.OpenUnion(dir)
}

// Exists says whether dir is a directory.
func Exists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
