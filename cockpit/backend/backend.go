// Package backend is the cockpit's extension API (ADR-008): what a backend implements so the three
// verbs can work against a kind of repository the cockpit itself does not know.
//
// A backend answers where and how: whether this is the repository its configuration belongs to,
// how written records become durable, and where their bytes can be fetched again. It never answers
// whether to believe a record. Verification against trust tiers stays in the cockpit, and a backend
// adds no verb (SPEC §13).
//
// The reference implementation ships with the cockpit: backend/github (git with a GitHub remote)
// and backend/local (a plain directory, ADR-004). An extension lives in its own module, registers
// itself from an init function, and is linked into a binary of its own. Go links statically, so
// that binary is the composition.
//
// Everything here uses only public types, so a module outside this repository can implement it.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Repo is a repository's configuration as a backend sees it.
type Repo struct {
	// Mode is repo.mode from cockpit.config.json; empty means "git".
	Mode string
	// Block is the whole `repo` object, so a backend reads the fields it defines itself.
	Block json.RawMessage
	// Dir is where cockpit.config.json was found. Once Bind succeeded, it is the repository.
	Dir string
	// Commit and Push are what the configuration asks for. A backend that has no such notion
	// ignores them.
	Commit, Push bool
	// SessionID is the configured session identity, for backends that attribute their own writes.
	SessionID string
}

// Decode reads the repo block into v.
func (r Repo) Decode(v any) error {
	if len(r.Block) == 0 {
		return nil
	}
	return json.Unmarshal(r.Block, v)
}

// Revision is what Persist made durable.
type Revision struct {
	// ID names the revision in the backend's own terms: a commit sha, a check-in id. Empty means
	// nothing was persisted, and then there is nothing to locate either.
	ID string
	// Rejected means the write happened locally but did not reach the shared place: a rejected
	// push, a conflicting check-in. It is not a failure of the record, which is signed and kept,
	// but the caller must be told.
	Rejected bool
	// Reason says why it was rejected, in the backend's words.
	Reason string
	// Per holds a revision per path, for backends that version each file on its own.
	Per map[string]string
}

// Backend is what an extension implements.
type Backend interface {
	// Validate checks the repo block alone, before anything touches the outside world.
	Validate(r Repo) error
	// Bind is the anti-wrong-folder guard (SPEC §5). It must compare the configuration against a
	// second source the configuration did not write, and refuse on any mismatch. It is called on
	// every verb, and nothing may be cached across calls.
	Bind(ctx context.Context, r Repo) error
	// Persist makes the given repository-relative paths durable and names the revision.
	Persist(ctx context.Context, r Repo, paths []string, message string) (Revision, error)
	// Locate gives the URI from which a path's bytes in a revision can be fetched, if there is one.
	// A locator is carried, not covered: it never changes a record's identity (kton §6.1).
	Locate(r Repo, rev Revision, path string) (uri string, ok bool)
}

var (
	mu       sync.RWMutex
	registry = map[string]Backend{}
)

// Register makes a backend available under a mode. It is meant to be called from an init function,
// and it panics on a second registration of the same mode: two implementations of one mode in one
// binary would make which one runs an accident of link order.
func Register(mode string, b Backend) {
	mu.Lock()
	defer mu.Unlock()
	if mode == "" {
		panic("backend: Register with an empty mode")
	}
	if _, dup := registry[mode]; dup {
		panic(fmt.Sprintf("backend: mode %q registered twice", mode))
	}
	registry[mode] = b
}

// Lookup returns the backend for a mode; the empty mode is "git".
func Lookup(mode string) (Backend, bool) {
	if mode == "" {
		mode = "git"
	}
	mu.RLock()
	defer mu.RUnlock()
	b, ok := registry[mode]
	return b, ok
}

// Modes lists the registered modes, sorted.
func Modes() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for m := range registry {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
