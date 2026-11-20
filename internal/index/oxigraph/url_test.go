//go:build docker

package oxigraph_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A configuration that names its index by URL keeps its view as a started daemon does: the second
// query finds it current instead of building it again (qm-dev-provenance, 20.11.: 25-37 s per ask).
func TestOxigraph_AnIndexByURLKeepsItsView(t *testing.T) {
	ctx := context.Background()
	idx := t.TempDir()
	t.Setenv("COCKPIT_INDEX_DIR", idx)
	r := testrepo.New(t)
	cfg := indexed(t, r)
	started, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	author(t, r, cfg, "one")

	byURL := indexed(t, r)
	byURL.Raw.Index.URL = started.URL
	d, err := oxigraph.Ensure(ctx, byURL)
	if err != nil {
		t.Fatal(err)
	}
	q := `SELECT ?a WHERE { ?a <http://www.w3.org/ns/prov#wasAttributedTo> ?k }`
	first, err := oxigraph.Free(ctx, d, byURL, keys(t, byURL), q, oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := oxigraph.Free(ctx, d, byURL, keys(t, byURL), q, oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Rows) != len(first.Rows) || len(first.Rows) == 0 {
		t.Errorf("by URL, both queries answer the same: first %d rows, second %d", len(first.Rows), len(second.Rows))
	}
	// The view built for the first query is kept for the URL, so the second one does not build it again.
	kept, _ := filepath.Glob(filepath.Join(idx, "url-*", "datasets", "*.json"))
	if len(kept) == 0 {
		t.Error("by URL, the built view is not kept: every query would build it again")
	}
}
