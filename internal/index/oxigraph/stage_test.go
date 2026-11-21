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
}
