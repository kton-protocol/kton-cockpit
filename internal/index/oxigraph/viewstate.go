package oxigraph

import (
	"context"
	"crypto/ed25519"
	"net/url"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// ViewState is what a view holds, as its mark graph says: the dataset key it was built under and the
// hash of each of its graphs (the record graph by its list, the rest, each scope graph). makeView
// writes the mark graph in the same update that puts the graphs in place, so one read of it is one
// view, whoever built it.
type ViewState struct {
	Slot string
	Key  string
	// Graphs maps each graph of the view to its hash; nil for a view built before the hashes were kept,
	// or none, whose graphs cannot be compared.
	Graphs map[string]string
}

// ReadViewState reads slot's view state. A slot with no view gives a state without graphs.
func (d *Daemon) ReadViewState(ctx context.Context, slot string) (ViewState, error) {
	q := "SELECT ?s ?p ?o WHERE { GRAPH <" + markIRI(slot) + "> { ?s ?p ?o } }"
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return ViewState{}, err
	}
	r, err := ParseResults(body)
	if err != nil {
		return ViewState{}, err
	}
	s := ViewState{Slot: slot}
	graphs := map[string]string{}
	hashed := false
	for _, row := range r.Rows {
		switch {
		case row["s"] == viewIRI(slot) && row["p"] == kNS+"key":
			s.Key = row["o"]
		case row["s"] == viewIRI(slot) && row["p"] == kNS+"defHash":
			graphs[viewIRI(slot)] = row["o"]
			hashed = true
		case row["p"] == kNS+"hash":
			graphs[row["s"]] = row["o"]
		}
	}
	if hashed {
		s.Graphs = graphs
	}
	return s, nil
}

// ViewChanges is what changed from one view state to the next: the graphs whose content changed or
// that came, the graphs that went, or All when the two cannot be compared graph by graph (another
// view, or one without hashes on either side).
type ViewChanges struct {
	Graphs, Removed []string
	All             bool
}

// Diff names what changed from old to s.
func (s ViewState) Diff(old ViewState) ViewChanges {
	if old.Slot != s.Slot || old.Graphs == nil || s.Graphs == nil {
		return ViewChanges{Graphs: []string{}, Removed: []string{}, All: true}
	}
	c := ViewChanges{Graphs: []string{}, Removed: []string{}}
	for g, h := range s.Graphs {
		if old.Graphs[g] != h {
			c.Graphs = append(c.Graphs, g)
		}
	}
	for g := range old.Graphs {
		if _, still := s.Graphs[g]; !still {
			c.Removed = append(c.Removed, g)
		}
	}
	sort.Strings(c.Graphs)
	sort.Strings(c.Removed)
	return c
}

// SlotFor names the view cfg's queries under keys read, without reading anything into the index.
func SlotFor(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey) (string, error) {
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return "", err
	}
	vc, err := ViewConfigFor(cfg)
	if err != nil {
		return "", err
	}
	slot, _ := datasetKey(inputs, keys, vc.Fingerprint())
	return slot, nil
}

// ScopeGraph names the graph of seed's claims (https://kton.dev/o/<hex>) in slot's view.
func ScopeGraph(slot, seed string) string {
	return scopeGraphIRI(slot, strings.TrimPrefix(seed, oNS))
}
