package index

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// memory keeps the opened union for as long as the process runs: the MCP server answers many calls
// and, without it, opens every registry again for each one.
//
// Noticing a change (ADR-012, "Open"): before every answer it stamps the registries the union is
// built from — every file's path, size and modification time, read with stat and never opened, so
// nothing here learns the store's layout (SPEC §12.3). An unchanged stamp means the held union is
// still what the registries say; any other stamp opens the union again, from the substrate, exactly
// as "none" does. A write in this process, a pull, a check-in elsewhere all change a file, so none
// of them needs to tell the index anything.
//
// It holds no trust: a tier is applied from the calling repository's configuration at answer
// time, so a configuration change needs nothing from here.
type memory struct {
	mu   sync.Mutex
	held map[string]*heldEntry // by repository and federation sources
}

type heldEntry struct {
	stamp  string
	union  *binaries.Union
	opened time.Time
}

// mem is the process's one memory index.
var mem = &memory{held: map[string]*heldEntry{}}

// memOpener is the Opener for one call. It remembers which held union it answered with, so the answer
// can say so.
type memOpener struct {
	m    *memory
	used *heldEntry
	kept bool
}

func (o *memOpener) Open(ctx context.Context, cfg *config.Config) (*binaries.Union, error) {
	e, kept, err := o.m.open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	o.used, o.kept = e, kept
	return e.union, nil
}

func (m *memory) open(ctx context.Context, cfg *config.Config) (*heldEntry, bool, error) {
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, false, err
	}
	stamp, err := Stamp(inputs)
	if err != nil {
		return nil, false, err
	}
	srcs, _ := json.Marshal(cfg.Raw.Federation.Sources)
	key := cfg.RepoRoot + "\x00" + string(srcs)

	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.held[key]; e != nil && e.stamp == stamp {
		return e, true, nil
	}
	u, err := binaries.OpenUnion(ctx, cfg)
	if err != nil {
		delete(m.held, key)
		return nil, false, err
	}
	// The union is held under the stamp taken BEFORE opening. A write landing in between then
	// leaves a stamp older than what was opened, and the next call opens again; holding the later
	// stamp could keep a union that misses that write. Opening a registry can itself write to it
	// (the kernel persists its numbering), which costs one more open, never a stale answer.
	e := &heldEntry{stamp: stamp, union: u, opened: time.Now().UTC()}
	m.held[key] = e
	return e, false, nil
}

// Forget drops every union held for the repository at root.
func Forget(root string) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	for k := range mem.held {
		if strings.HasPrefix(k, root+"\x00") {
			delete(mem.held, k)
		}
	}
}

// Stamp is binaries.Stamp: the union cache and this index notice a change by the same rule.
func Stamp(inputs []binaries.Input) (string, error) { return binaries.Stamp(inputs) }

// Answered says where an answer came from (ADR-012: an answer states whether it came from a derived
// index). It is nil for "none", whose answers are read from the registries and say nothing new.
type Answered struct {
	Kind string `json:"kind"`
	// Kept is true when the union was the one held from an earlier call, unchanged since Opened.
	Kept   bool   `json:"kept"`
	Opened string `json:"opened"`
}

// memReader is a Runner whose union comes from the memory index.
type memReader struct {
	*binaries.Runner
	o *memOpener
}

func (r memReader) Answered() *Answered {
	if r.o.used == nil {
		return nil
	}
	return &Answered{Kind: "memory", Kept: r.o.kept, Opened: r.o.used.opened.Format(time.RFC3339Nano)}
}
