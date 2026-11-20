//go:build docker

package oxigraph_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A load never writes over a graph the index holds that no catalogue lists: a foton's record graph has
// the same namespace. The load is refused and the foton is still answered (kton-review L1 on #192).
func TestLoadGraph_NeverWritesOverARecordsGraph(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	cfg.Raw.Index.Derived.Enabled = true
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	f := author(t, r, cfg, "one")
	const attributed = `SELECT ?a WHERE { ?a <http://www.w3.org/ns/prov#wasAttributedTo> ?k }`
	if a := query(t, d, cfg, attributed); !holds(a, "a", f) {
		t.Fatalf("want f read in: %+v", a)
	}
	_, err = d.LoadGraph(ctx, cfg, strings.TrimPrefix(f, "sha256:"), []byte("<https://x.org/s> <https://x.org/p> 1 .\n"), "application/n-triples", "rdf/x.nt")
	if !errors.Is(err, oxigraph.ErrClash) {
		t.Fatalf("a load over a foton's record graph: want ErrClash, got %v", err)
	}
	if a := query(t, d, cfg, attributed); !holds(a, "a", f) {
		t.Errorf("after the refused load the foton must still be answered: %+v", a)
	}
}
