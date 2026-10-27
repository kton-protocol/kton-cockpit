//go:build docker

package oxigraph_test

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A configuration that asks for more memory than the daemon was started with restarts it on the
// same store, and nothing read in is lost; one that asks for less uses it as it is.
func TestOxigraph_ALargerMemoryLimitRestartsTheDaemonOnItsStore(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	cfg.Raw.Index.Memory = "1g"
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

	cfg.Raw.Index.Memory = "512m"
	if d2, err := oxigraph.Ensure(ctx, cfg); err != nil || d2.URL != d.URL {
		t.Fatalf("a smaller limit: want the running daemon, got %v %v", d2, err)
	}
	if _, l, _ := oxigraph.Running(ctx); l == nil || l.MemoryOrDefault() != "1g" {
		t.Fatalf("a smaller limit: want it left at 1g, got %+v", l)
	}

	cfg.Raw.Index.Memory = "2g"
	d3, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, l, _ := oxigraph.Running(ctx); l == nil || l.MemoryOrDefault() != "2g" {
		t.Fatalf("a larger limit: want it restarted with 2g, got %+v", l)
	}
	if a := query(t, d3, cfg, attributed); !holds(a, "a", f) || !a.Kept {
		t.Fatalf("restarted on its store: want f without reading in again: %+v", a)
	}
}
