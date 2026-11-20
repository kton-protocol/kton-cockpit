package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nclaim "kton.dev/nekton/claim"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// Events a frontend gets over serve --stdio when it asks for them (SPEC §12.5): what changed in the
// registries this repository reads, whoever changed them, in the order a frontend needs to act on
// it. The position is the feed's cursor (SPEC §9.10), so a frontend that restarts serve resumes where
// it was, and ktonwatch's /v1/watch and these events share one notion of position.
const (
	EventPublished    = "events/published"
	EventSaid         = "events/said"
	EventSourcePulled = "events/sourcePulled"
	EventIndexMoved   = "events/indexMoved"
)

// PublishedEvent names the steps new in this repository's own registry.
type PublishedEvent struct {
	IDs    []string `json:"ids"`
	Cursor string   `json:"cursor"`
	// Truncated says more arrived than one event carries: query again rather than wait for the rest.
	Truncated bool `json:"truncated,omitempty"`
}

// SaidEvent names the claims new in this repository's own registry, each with what a frontend needs
// to decide whether it shows it, without a query.
type SaidEvent struct {
	IDs       []string    `json:"ids"`
	Claims    []SaidClaim `json:"claims"`
	Cursor    string      `json:"cursor"`
	Truncated bool        `json:"truncated,omitempty"`
}

// SaidClaim is one new claim: its scope (seed and name), its template where this repository knows
// it, its predicate, what it is about, and the trusted keys that signed it.
type SaidClaim struct {
	ID        string      `json:"id"`
	Scope     *EventScope `json:"scope,omitempty"`
	Template  string      `json:"template,omitempty"`
	Predicate string      `json:"predicate,omitempty"`
	About     []string    `json:"about,omitempty"`
	Signers   []string    `json:"signers"`
}

// EventScope names a scope by its seed and the name the seed gives it.
type EventScope struct {
	Seed  string `json:"seed"`
	Label string `json:"label,omitempty"`
}

// SourcePulledEvent says a federation source holds records it did not hold before.
type SourcePulledEvent struct {
	Source string `json:"source"`
	NewIDs int    `json:"newIds"`
	// Head is the source's revision where its kind has one (a git source's commit).
	Head   string `json:"head,omitempty"`
	Cursor string `json:"cursor"`
}

// IndexMovedEvent follows every published, said and sourcePulled event once a query sees what they
// name. Scopes are the scopes whose claims changed; empty means everything (records, or a source).
// Slot names the view a query reads, so a frontend showing one view ignores another's moves.
type IndexMovedEvent struct {
	Slot   string       `json:"slot"`
	Scopes []EventScope `json:"scopes"`
}

// EventsCursor is the present as a cursor: events from it on name only what arrives later.
func (c *Cockpit) EventsCursor(ctx context.Context) (string, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return "", err
	}
	w := &watcher{cfg: cfg}
	for {
		page, err := w.page(ctx)
		if err != nil {
			return "", err
		}
		w.cursor = page.Cursor
		if !page.More {
			return w.cursor, nil
		}
	}
}

// Watch sends the events of the repository c serves to emit, every interval, until ctx ends, from
// since: a cursor an earlier connection's events carried, or EventsCursor's present.
func (c *Cockpit) Watch(ctx context.Context, since string, interval time.Duration, emit func(method string, params any)) error {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return err
	}
	if since == "" {
		if since, err = c.EventsCursor(ctx); err != nil {
			return err
		}
	}
	w := &watcher{cfg: cfg, emit: emit, labels: map[string]string{}, cursor: since}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := w.tick(ctx); err != nil && ctx.Err() == nil {
			// A tick that failed (a registry being written, the index restarting) is tried again at the
			// next one; the cursor did not move, so nothing is lost.
			_ = err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

type watcher struct {
	cfg    *config.Config
	emit   func(string, any)
	cursor string
	labels map[string]string // seed -> its name
	// owed says events went out whose indexMoved has not: the index was not brought up to date yet
	// (a backlog still being read, or the index failed). It stays owed, with the scopes that changed,
	// until an update succeeds, whatever the ticks in between bring (kton-review E1 on #213).
	owed       bool
	everything bool
	scopes     map[string]bool
	// update brings the index up to date and names the view; current, unless a test replaces it.
	update func(context.Context) (string, error)
}

// CheckEventsCursor refuses a since that is no cursor this cockpit hands out: a connection would
// otherwise never get an event (kton-review E2 on #213).
func (c *Cockpit) CheckEventsCursor(ctx context.Context, since string) error {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return err
	}
	_, err = askFeed(ctx, cfg, AskRequest{Query: "feed", Cursor: since, Limit: 1})
	var ref *Refusal
	if errors.As(err, &ref) && ref.Code == "argument" {
		return refuse("events.cursor", "SPEC §12.5", "since is not a cursor this cockpit's events handed out")
	}
	return err
}

func (w *watcher) page(ctx context.Context) (*FeedPage, error) {
	res, err := askFeed(ctx, w.cfg, AskRequest{Query: "feed", Cursor: w.cursor})
	if err != nil {
		return nil, err
	}
	return res.Feed, nil
}

func (w *watcher) tick(ctx context.Context) error {
	page, err := w.page(ctx)
	if err != nil {
		return err
	}
	if page.Cursor != w.cursor {
		// Untrusted records move the cursor too: no event names them, but the view may have changed.
		w.owed = true
	}
	if len(page.Records) == 0 {
		w.cursor = page.Cursor
		return w.indexMoved(ctx, page.More)
	}
	run := binaries.New(w.cfg)
	byPred := map[string]string{}
	if set, _, err := run.TemplatesAndAliases(); err == nil {
		for _, n := range set.Names() {
			if t, ok := set.Get(n); ok && t.Predicate != "" {
				byPred[set.Resolve(t.Predicate)] = n
			}
		}
	}
	var published PublishedEvent
	said := SaidEvent{Claims: []SaidClaim{}}
	sources := map[string]int{}
	var order []string
	if w.scopes == nil {
		w.scopes = map[string]bool{}
	}
	for _, r := range page.Records {
		switch {
		case r.Registry != "own":
			if sources[r.Registry] == 0 {
				order = append(order, r.Registry)
			}
			sources[r.Registry]++
			w.everything = true
		case r.Kind == "foton":
			published.IDs = append(published.IDs, r.ID)
			w.everything = true // steps are in no scope graph
		default:
			sc := w.claim(ctx, run, r, byPred)
			said.IDs = append(said.IDs, r.ID)
			said.Claims = append(said.Claims, sc)
			if sc.Scope != nil {
				w.scopes[sc.Scope.Seed] = true
			} else {
				w.everything = true
			}
		}
	}
	if published.IDs != nil {
		published.Cursor, published.Truncated = page.Cursor, page.More
		w.emit(EventPublished, published)
	}
	if said.IDs != nil {
		said.Cursor, said.Truncated = page.Cursor, page.More
		w.emit(EventSaid, said)
	}
	for _, name := range order {
		w.emit(EventSourcePulled, SourcePulledEvent{Source: name, NewIDs: sources[name], Head: w.head(name), Cursor: page.Cursor})
	}
	w.cursor = page.Cursor
	w.owed = true
	return w.indexMoved(ctx, page.More)
}

// indexMoved sends what is owed once a query sees it: the index is brought up to date first, once,
// after a backlog is read to its end rather than per page.
func (w *watcher) indexMoved(ctx context.Context, more bool) error {
	if !w.owed || more {
		return nil
	}
	update := w.update
	if update == nil {
		update = w.current
	}
	slot, err := update(ctx)
	if err != nil {
		return err // still owed: the next tick tries again
	}
	ev := IndexMovedEvent{Slot: slot, Scopes: []EventScope{}}
	if !w.everything {
		for seed := range w.scopes {
			ev.Scopes = append(ev.Scopes, EventScope{Seed: seed, Label: w.labels[seed]})
		}
		sort.Slice(ev.Scopes, func(i, j int) bool { return ev.Scopes[i].Seed < ev.Scopes[j].Seed })
	}
	w.emit(EventIndexMoved, ev)
	w.owed, w.everything, w.scopes = false, false, map[string]bool{}
	return nil
}

// claim reads a new claim for its event entry.
func (w *watcher) claim(ctx context.Context, run *binaries.Runner, r FeedRecord, byPred map[string]string) SaidClaim {
	sc := SaidClaim{ID: r.ID, Signers: []string{}}
	for _, s := range r.Signers {
		sc.Signers = append(sc.Signers, s.KeyID)
	}
	var env core.Envelope
	if json.Unmarshal(r.Envelope, &env) != nil {
		return sc
	}
	st, _, err := nclaim.ParseEnvelope(env)
	if err != nil {
		return sc
	}
	for _, s := range st.Subject {
		if h := s.Digest["sha256"]; h != "" {
			sc.About = append(sc.About, "sha256:"+h)
		} else if s.URI != "" {
			sc.About = append(sc.About, s.URI)
		}
	}
	var p nclaim.Predicate
	if json.Unmarshal(st.Predicate, &p) != nil {
		return sc
	}
	sc.Predicate = p.Predicate.URI
	if sc.Predicate == "" && p.Predicate.Hash != "" {
		sc.Predicate = p.Predicate.Hash
	}
	sc.Template = byPred[sc.Predicate]
	if p.Scope != "" {
		seed := "https://kton.dev/o/" + strings.TrimPrefix(strings.ToLower(p.Scope), "sha256:")
		if _, ok := w.labels[seed]; !ok {
			name := ""
			if s, err := run.Seed(ctx, p.Scope); err == nil {
				name = s.Name
			}
			w.labels[seed] = name
		}
		sc.Scope = &EventScope{Seed: seed, Label: w.labels[seed]}
	}
	return sc
}

// head is a git source's commit, read from its local copy; "" for a kind without one.
func (w *watcher) head(name string) string {
	cache, err := binaries.SourceCache(w.cfg, name)
	if err != nil {
		return ""
	}
	out, err := exec.Command("git", "-C", filepath.Join(cache, "clone"), "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// current brings the index up to date with the registries, so the next query sees them, and names
// the view; without an index each query reads the registries as they are, and the slot is "".
func (w *watcher) current(ctx context.Context) (string, error) {
	if w.cfg.Raw.Index.Kind != "oxigraph" {
		return "", nil
	}
	d, err := oxigraph.Ensure(ctx, w.cfg)
	if err != nil {
		return "", err
	}
	keys, err := trustedKeys(w.cfg)
	if err != nil {
		return "", err
	}
	a, err := oxigraph.Free(ctx, d, w.cfg, keys, "ASK { }", oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		return "", err
	}
	return a.Slot, nil
}
