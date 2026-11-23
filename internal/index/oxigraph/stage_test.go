//go:build docker

package oxigraph_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A view goes up in many graph-store requests (here 64 bytes each, so every claim is a request of its
// own) and is put in place by one small update: every claim is in it, and no staging graph is left
// (the engine's 128 MiB body limit; kton-coordination and userinterface, 21.11.).
func TestOxigraph_AViewGoesUpInChunks(t *testing.T) {
	defer oxigraph.SetStageChunk(64)()
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	for i := 0; i < 40; i++ {
		if _, err := binaries.New(cfg).Annotate(ctx, fmt.Sprintf("urn:example:%d", i), "working-on",
			map[string]string{"step": fmt.Sprint(i), "by-session": testrepo.SessionID}, cfg.NektonKey, nil); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) string {
		t.Helper()
		a, err := oxigraph.Free(ctx, d, cfg, keys(t, cfg), q, oxigraph.DatasetClaims, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return a.Rows[0]["n"]
	}
	if n := count(`SELECT (COUNT(DISTINCT ?c) AS ?n) WHERE { ?c <https://kton.dev/v/predicate> ?p ; <https://kton.dev/v/about> ?s }`); n != "40" {
		t.Errorf("every claim in the view: want 40, got %s", n)
	}
	// The store's own dataset: any staging graph left behind would show here.
	left, err := d.Count(ctx, `SELECT (COUNT(*) AS ?n) WHERE { GRAPH ?g { ?s ?p ?o } FILTER(CONTAINS(STR(?g), ":staging:")) }`)
	if err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("no staging graph left: %d triples", left)
	}

	// A staging graph a killed build left behind is swept by the next build of the view (kton-review S1).
	a, err := oxigraph.Free(ctx, d, cfg, keys(t, cfg), "ASK { }", oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stale := oxigraph.ViewIRI(a.Slot) + ":claims:staging:dead"
	if err := d.Stage(ctx, stale, []oxigraph.Quad{{S: oxigraph.Term{Type: "uri", Value: "urn:x"},
		P: oxigraph.Term{Type: "uri", Value: "urn:y"}, O: oxigraph.Term{Type: "literal", Value: "z"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := binaries.New(cfg).Annotate(ctx, "urn:example:more", "working-on",
		map[string]string{"step": "more", "by-session": testrepo.SessionID}, cfg.NektonKey, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT (COUNT(DISTINCT ?c) AS ?n) WHERE { ?c <https://kton.dev/v/predicate> ?p ; <https://kton.dev/v/about> ?s }`); n != "41" {
		t.Errorf("the rebuilt view: want 41 claims, got %s", n)
	}
	if left, _ := d.Count(ctx, `SELECT (COUNT(*) AS ?n) WHERE { GRAPH ?g { ?s ?p ?o } FILTER(CONTAINS(STR(?g), ":staging:")) }`); left != 0 {
		t.Errorf("the stale staging graph is swept: %d triples left", left)
	}
}

// doctor's warning reads how much of its limit a running daemon uses (userinterface, 21.11.).
func TestOxigraph_MemoryInUseOfARunningDaemon(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	if _, err := oxigraph.Ensure(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	p, limit, ok := oxigraph.MemoryInUse(ctx, cfg)
	if !ok || p <= 0 || p > 100 || limit != "2g" {
		t.Errorf("a running daemon's memory: %v %q %v", p, limit, ok)
	}
}
