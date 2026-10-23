//go:build docker

package oxigraph_test

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A query against registries that did not move uses the dataset kept for them, and a kept dataset
// never hides what changed: a new record, other trusted keys, or a store emptied under it.
func TestOxigraph_AKeptDatasetNeverHidesAChange(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	const attributed = `SELECT ?a WHERE { ?a <http://www.w3.org/ns/prov#wasAttributedTo> ?k }`

	f1 := author(t, r, cfg, "one")
	if a := query(t, d, cfg, attributed); !holds(a, "a", f1) || a.Kept {
		t.Fatalf("first query: want f1, read in: %+v", a)
	}
	if a := query(t, d, cfg, attributed); !holds(a, "a", f1) || !a.Kept {
		t.Fatalf("nothing moved: want f1 from the kept dataset: %+v", a)
	}

	f2 := author(t, r, cfg, "two")
	if a := query(t, d, cfg, attributed); !holds(a, "a", f2) || a.Kept {
		t.Fatalf("a new record: want f2, read in: %+v", a)
	}

	a, err := oxigraph.Query(ctx, d, cfg, nil, attributed)
	if err != nil || len(a.Rows) != 0 {
		t.Fatalf("no trusted key: want nothing attributed, got %+v %v", a, err)
	}

	if err := d.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if a := query(t, d, cfg, attributed); !holds(a, "a", f1) || !holds(a, "a", f2) {
		t.Fatalf("a store emptied under a kept dataset: want both read in again: %+v", a)
	}
}
