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
	// Commit and Share are what the configuration asks for (persist.commit, persist.share): make
	// durable, and share with the place others read. A backend that has no such notion
	// ignores them.
	Commit, Share bool
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

// Binder answers "is this the repository the configuration belongs to".
type Binder interface {
	// Root names the repository that contains dir, in this backend's terms (git: the working tree's
	// top level). The configuration is looked for in dir and, one step only, in the root a backend
	// names here — never in an arbitrary parent (SPEC §5.1). A backend that has no notion of a
	// containing repository returns an error.
	Root(ctx context.Context, dir string) (string, error)
	// Private lists the repository-relative paths the backend keeps for itself (git: ".git"). The
	// cockpit never publishes them, never lets a configured directory lie in them, and hides them
	// from an executed command.
	Private() []string
	// Validate checks the repo block alone, before anything touches the outside world.
	Validate(r Repo) error
	// Bind is the anti-wrong-folder guard (SPEC §5). It must compare the configuration against a
	// second source the configuration did not write, and refuse on any mismatch. It is called on
	// every verb, and nothing may be cached across calls.
	Bind(ctx context.Context, r Repo) error
}

// Persister makes written records durable.
type Persister interface {
	// Persist makes the given repository-relative paths durable and names the revision.
	Persist(ctx context.Context, r Repo, paths []string, message string) (Revision, error)
}

// Locator says where a written path's bytes can be fetched again.
type Locator interface {
	// Locate gives the URI from which a path's bytes in a revision can be fetched, if there is one.
	// A locator is carried, not covered: it never changes a record's identity (kton §6.1).
	Locate(r Repo, rev Revision, path string) (uri string, ok bool)
}

// The parts below are optional: a backend implements them where its store has the notion, and a
// caller that finds one missing says so instead of falling back to some other backend's way.

// Syncer follows a shared place: what the repository's current revision is, and exchanging what
// was made durable there and here since.
type Syncer interface {
	// Head is the current revision. It is cheap, so the index can ask it before every answer.
	Head(ctx context.Context, r Repo) (Revision, error)
	// Sync takes in what others shared, then shares what is durable here only (when r.Share), and
	// returns the new head. It never rewrites a local revision: locators already signed into records
	// pin them. Where both sides changed the same thing it refuses and leaves everything as it was.
	// A share the shared place refuses comes back as Revision.Rejected, as from Persist.
	Sync(ctx context.Context, r Repo) (Revision, error)
}

// State is what a path is to the store.
type State int

const (
	// Untracked: the store does not hold the path.
	Untracked State = iota
	// Durable: the store holds it as it is on disk.
	Durable
	// Changed: the store holds it, and the file on disk differs.
	Changed
)

func (s State) String() string {
	return [...]string{"untracked", "durable", "changed"}[s]
}

// Inspector reports what the store holds of given paths.
type Inspector interface {
	Status(ctx context.Context, r Repo, paths []string) (map[string]State, error)
}

// Fetcher gives back the bytes behind a locator another backend's Locate made: the reverse of
// Locate, chosen by the locator itself, so it needs no configured repository. The caller checks the
// bytes against the recorded hash; a Fetcher only fetches.
type Fetcher interface {
	// Handles reports whether this backend can fetch uri.
	Handles(uri string) bool
	Fetch(ctx context.Context, uri string) ([]byte, error)
}

// Backend is what an extension implements for a repo mode: the three parts together (ADR-011).
// Execution is not part of it; that is cockpit/executor, registered on its own, so a bundle such as
// improve brings both and git brings only this.
type Backend interface {
	Binder
	Persister
	Locator
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

// FetcherFor is the first registered backend, in mode order, that fetches uri.
func FetcherFor(uri string) (Fetcher, bool) {
	for _, m := range Modes() {
		b, _ := Lookup(m)
		if f, ok := b.(Fetcher); ok && f.Handles(uri) {
			return f, true
		}
	}
	return nil, false
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
