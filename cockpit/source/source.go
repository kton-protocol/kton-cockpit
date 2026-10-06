// Package source is where federated records come from (cockpit.config.json `federation.sources`).
//
// The kernels know nothing about where a registry lives — a folder, a repository, a document
// store are all just places holding signed records. So "which other registries does this cockpit
// read together with its own" is the cockpit's to say, and HOW a kind of place is read is a source
// kind, registered the same way a repo backend is (ADR-008): the core ships `dir`, an extension
// brings its own (cockpit-improve brings `improve`).
//
// A source is read-only. Its records join the union this cockpit reads — ask, show, search — and
// nothing is ever written back. Trust is unchanged by it: every record still verifies against the
// configured tiers, which stay the ceiling; a source adds records, never signers.
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
)

// Spec is one configured source: a name, a kind, and the kind's own fields.
type Spec struct {
	Name  string
	Kind  string
	Block json.RawMessage
}

// UnmarshalJSON keeps the whole object as the kind's block.
func (s *Spec) UnmarshalJSON(b []byte) error {
	var head struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	s.Name, s.Kind, s.Block = head.Name, head.Kind, append(json.RawMessage(nil), b...)
	return nil
}

// MarshalJSON writes the block back as it was.
func (s Spec) MarshalJSON() ([]byte, error) { return s.Block, nil }

// Decode reads the kind's fields.
func (s Spec) Decode(v any) error { return json.Unmarshal(s.Block, v) }

// Registry is a source's two registries as local directories, ready to be opened.
type Registry struct {
	PlanktonDir string
	NektonDir   string
}

// Kind reads one kind of place.
type Kind interface {
	// Validate checks the spec alone, at load.
	Validate(s Spec) error
	// Fetch makes the source's registries readable locally — one or more: a place may hold several
	// participants side by side (a site and the packages installed in it), each with its own
	// records, none copied into another. base is the repository root (for relative paths); cache is
	// a directory this source may keep a copy in between calls.
	Fetch(ctx context.Context, s Spec, base, cache string) ([]Registry, error)
}

var (
	mu    sync.RWMutex
	kinds = map[string]Kind{}
)

// Register makes a kind available. A second registration of the same name is a programming error.
func Register(name string, k Kind) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := kinds[name]; dup {
		panic("source kind registered twice: " + name)
	}
	kinds[name] = k
}

// Lookup finds a kind.
func Lookup(name string) (Kind, bool) {
	mu.RLock()
	defer mu.RUnlock()
	k, ok := kinds[name]
	return k, ok
}

// Kinds lists the registered kinds.
func Kinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate checks a list of specs: names present and unique, kinds known, each kind content.
func Validate(specs []Spec) error {
	seen := map[string]bool{}
	for i, s := range specs {
		if s.Name == "" {
			return fmt.Errorf("federation.sources[%d] has no name", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("federation.sources: %q is named twice", s.Name)
		}
		seen[s.Name] = true
		k, ok := Lookup(s.Kind)
		if !ok {
			return fmt.Errorf("federation.sources %q: kind %q is not available in this build (available: %v)", s.Name, s.Kind, Kinds())
		}
		if err := k.Validate(s); err != nil {
			return fmt.Errorf("federation.sources %q: %w", s.Name, err)
		}
	}
	return nil
}

// dir is a registry in a directory: `path` holding registry/plankton and registry/nekton, the layout
// every cockpit repository and every site has.
type dir struct{}

type dirBlock struct {
	Path string `json:"path"`
}

func (dir) Validate(s Spec) error {
	var b dirBlock
	if err := s.Decode(&b); err != nil {
		return err
	}
	if b.Path == "" {
		return fmt.Errorf("kind dir needs \"path\" (a directory holding registry/plankton and registry/nekton)")
	}
	return nil
}

func (dir) Fetch(_ context.Context, s Spec, base, _ string) ([]Registry, error) {
	var b dirBlock
	if err := s.Decode(&b); err != nil {
		return nil, err
	}
	p := b.Path
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return []Registry{{PlanktonDir: filepath.Join(p, "registry", "plankton"), NektonDir: filepath.Join(p, "registry", "nekton")}}, nil
}

func init() { Register("dir", dir{}) }
