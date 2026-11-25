//go:build docker

package oxigraph_test

import (
	"context"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The index lists the registries it holds (architecture view G10): each with its directory, records,
// keys, position and cursor, whether it moved since, and which repositories' queries read it. A query
// notes its read at most once a minute per repository, so a warm query writes nothing.
func TestOxigraph_ListsItsRegistriesAndWhoReadsThem(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	author(t, r, cfg, "one")

	// One fixed minute for both queries, so a minute boundary between them cannot make two notes.
	at := time.Now()
	t.Cleanup(oxigraph.SetReadClock(func() time.Time { return at }))
	before := oxigraph.Notes()
	query(t, d, cfg, "SELECT * WHERE { ?s ?p ?o } LIMIT 1")
	if n := oxigraph.Notes() - before; n != 1 {
		t.Fatalf("the first query: %d read note(s) written, want 1", n)
	}
	query(t, d, cfg, "SELECT * WHERE { ?s ?p ?o } LIMIT 1")
	if n := oxigraph.Notes() - before; n != 1 {
		t.Errorf("a warm query in the same minute wrote a read note (%d in all)", n)
	}

	regs, err := d.RegistryList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 2 {
		t.Fatalf("want the repository's plankton and nekton registries, got %+v", regs)
	}
	var records int
	for _, g := range regs {
		if g.Dir == "" || len(g.Position) == 0 || g.Stale == nil || *g.Stale {
			t.Errorf("a registry read just now: dir, position, and not stale: %+v", g)
		}
		if len(g.ReadBy) != 1 || g.ReadBy[0].Root != cfg.RepoRoot || g.ReadBy[0].LastRead == "" {
			t.Errorf("read by this repository: %+v", g.ReadBy)
		}
		records += g.Records
		if g.Records > 0 && len(g.Keys) == 0 {
			t.Errorf("a registry with records names no key: %+v", g)
		}
	}
	if records == 0 {
		t.Errorf("the authored record is not counted: %+v", regs)
	}

	// A registry that moved is stale until the next query reads it.
	author(t, r, cfg, "two")
	regs, err = d.RegistryList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale := 0
	for _, g := range regs {
		if g.Stale != nil && *g.Stale {
			stale++
		}
	}
	if stale != 1 {
		t.Errorf("after a new record, want one stale registry: %+v", regs)
	}
}
