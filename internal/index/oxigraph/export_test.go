package oxigraph

import (
	"crypto/ed25519"

	"context"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"net/url"
	"time"
)

// SetStageChunk sets how much one graph-store request carries, for a test; it returns the undo.
func SetStageChunk(n int) func() {
	was := stageChunk
	stageChunk = n
	return func() { stageChunk = was }
}

// Count answers a COUNT query over the store's own dataset, for a test.
func (d *Daemon) Count(ctx context.Context, q string) (int64, error) { return d.count(ctx, q) }

// Stage writes triples into graph g through the graph store, for a test.
func (d *Daemon) Stage(ctx context.Context, g string, qs []Quad) error { return d.stage(ctx, g, qs) }

// ViewIRI names slot's view, for a test.
func ViewIRI(slot string) string { return viewIRI(slot) }

// Staged is how many graphs were uploaded so far, for a test.
func Staged() int64 { return staged.Load() }

// SetDuringBuild sets what runs once a build has marked itself.
func SetDuringBuild(f func()) { duringBuild = f }

func Notes() int64 { return notes.Load() }

// SetReadClock fixes the time read notes carry; the returned func restores it.
func SetReadClock(f func() time.Time) func() {
	old := readClock
	readClock = f
	return func() { readClock = old }
}

// IndexRows runs a SELECT over the store's own dataset (the index graph and the views), for a test.
func (d *Daemon) IndexRows(ctx context.Context, q string) ([]map[string]string, error) {
	return d.selectCSV(ctx, url.Values{"query": {q}})
}

// RegIRI names a registry directory in the index, for a test.
func RegIRI(dir string) string { return regIRI(dir) }

// ChainIRI names slot's chain graph, for a test.
func ChainIRI(slot string) string { return chainIRI(slot) }

// Builds says how many views were built whole and how many were patched so far, for a test.
func Builds() (full, patched int64) { return fullBuilds.Load(), patchBuilds.Load() }

// QuadGraphsRead is how many graphs the claim projection read out of the store so far, for a test.
func QuadGraphsRead() int64 { return quadGraphsRead.Load() }

// LastFallback is why the last patch gave way to a full build ("" when the view was just not patchable).
func LastFallback() string { s, _ := lastFallback.Load().(string); return s }

// GraphQuads reads graph g's triples, for a test.
func (d *Daemon) GraphQuads(ctx context.Context, g string) ([]Quad, error) {
	return d.quadsOf(ctx, []string{g})
}

// QuadKey identifies a triple as the patch compares them, for a test.
func QuadKey(q Quad) string { return quadKey(q) }

// ViewGraphs names the graphs of slot's view a test compares: the record graph, the union of the claims,
// the rest, the chain graph and every scope graph its metadata names.
func (d *Daemon) ViewGraphs(ctx context.Context, slot string) ([]string, error) {
	gs, err := d.claimGraphs(ctx, slot)
	if err != nil {
		return nil, err
	}
	return append([]string{viewIRI(slot), claimsIRI(slot), restIRI(slot), chainIRI(slot)}, gs...), nil
}

// PreparedView is what prepare answered for a view, for a test.
type PreparedView struct {
	Slot           string
	Named, Scopes  []string
	Graphs         []ClaimGraph
	Registries     int
	KeptWholeQuery bool
}

// Prepare brings cfg's view under keys up to date and says what it holds, for a test.
func (d *Daemon) Prepare(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey) (*PreparedView, error) {
	p, err := d.prepare(ctx, cfg, keys)
	if err != nil {
		return nil, err
	}
	return &PreparedView{p.slot, p.named, p.scopes, p.graphs, p.registries, p.kept}, nil
}
