//go:build docker

package cockpit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// loadingRepo is a repository with an Oxigraph index that lets loads in.
func loadingRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = "oxigraph"
	raw.Index.Derived.Enabled = true
	r.WriteConfig(t, raw)
	r.Use(t)
	t.Cleanup(func() { oxigraph.Stop(context.Background(), r.Config(t)) })
	return r
}

func codeOf(err error) string {
	var ref *Refusal
	if errors.As(err, &ref) {
		return ref.Code
	}
	return ""
}

const turtle = `@prefix ex: <https://example.org/> .
ex:element1 ex:outcome "Covered" .
ex:element2 ex:outcome "Gap" .
`

// A loaded file is a graph named by its hash, reached only through GRAPH, never in the claim view; it is
// listed, and dropped on request (ADR-017, SPEC §12.6).
func TestLoad_AFileIsAGraphNamedByItsHash(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	r.Write(t, "rdf/coverage.ttl", turtle)
	hex := strings.TrimPrefix(core.HashBytes([]byte(turtle)), "sha256:")

	out, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/coverage.ttl"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Loaded) != 1 || out.Loaded[0].Graph != "https://kton.dev/o/"+hex || out.Loaded[0].Triples != 2 || out.Loaded[0].MediaType != "text/turtle" {
		t.Fatalf("load: %+v", out)
	}
	ask := func(q string) *SparqlAnswer {
		t.Helper()
		a, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q})
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return a.Sparql
	}
	a := ask(`SELECT ?g ?e WHERE { GRAPH ?g { ?e <https://example.org/outcome> "Gap" } }`)
	if len(a.Rows) != 1 || a.Rows[0]["g"] != "https://kton.dev/o/"+hex || a.Rows[0]["e"] != "https://example.org/element2" || a.LoadedGraphs != 1 {
		t.Errorf("GRAPH ?g finds the fact with its hash: %+v", a)
	}
	if a := ask(`SELECT ?e WHERE { ?e <https://example.org/outcome> ?o }`); len(a.Rows) != 0 {
		t.Errorf("a loaded fact must not be in the claim view (the default graph): %+v", a.Rows)
	}
	l, err := c.LoadedGraphs(ctx)
	if err != nil || len(l.Loaded) != 1 || l.Loaded[0].From != "rdf/coverage.ttl" {
		t.Errorf("list: %+v %v", l, err)
	}
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/coverage.ttl"}}); err != nil {
		t.Errorf("loading the same file again: %v", err)
	}
	if err := c.DropLoaded(ctx, "sha256:"+hex); err != nil {
		t.Fatal(err)
	}
	if a := ask(`SELECT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`); len(a.Rows) != 0 {
		t.Errorf("after drop the graph is still seen: %+v", a.Rows)
	}
	if l, _ := c.LoadedGraphs(ctx); len(l.Loaded) != 0 || len(l.Missing) != 0 {
		t.Errorf("after drop the list still names it: %+v", l)
	}
}

// What a load refuses: loading off, a type it does not read, quads, a hash not in the content store,
// a file outside the repository, RDF/XML with entities, a file past maxBytes, unparsable content.
func TestLoad_Refuses(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	r.Write(t, "rdf/a.ttl", turtle)
	r.Write(t, "rdf/a.txt", turtle)
	r.Write(t, "rdf/bomb.rdf", `<?xml version="1.0"?><!DOCTYPE r [<!ENTITY a "aaaa">]><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>`)
	r.Write(t, "rdf/broken.ttl", "this is not turtle {")
	for _, c2 := range []struct {
		in   LoadRequest
		code string
	}{
		{LoadRequest{Files: []string{"rdf/a.txt"}}, "derived.type"},
		{LoadRequest{Files: []string{"rdf/a.ttl"}, As: "application/n-quads"}, "derived.quads"},
		{LoadRequest{Files: []string{"sha256:" + strings.Repeat("ab", 32)}, As: "text/turtle"}, "derived.missing"},
		{LoadRequest{Files: []string{"../outside.ttl"}}, "path.denied"},
		{LoadRequest{Files: []string{"rdf/bomb.rdf"}}, "derived.refused"},
		{LoadRequest{Files: []string{"rdf/broken.ttl"}}, "derived.refused"},
	} {
		if _, err := c.Load(ctx, c2.in); codeOf(err) != c2.code {
			t.Errorf("%+v: want %s, got %v", c2.in, c2.code, err)
		}
	}
	raw := r.Config(t).Raw
	raw.Index.Derived.MaxBytes = 10
	r.WriteConfig(t, raw)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/a.ttl"}}); codeOf(err) != "derived.too-large" {
		t.Errorf("past maxBytes: %v", err)
	}
	raw.Index.Derived = config.Derived{}
	r.WriteConfig(t, raw)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/a.ttl"}}); codeOf(err) != "derived.disabled" {
		t.Errorf("loading off: %v", err)
	}
}

// After the index is emptied, a query loads again what the checkout's load list names; a file that is
// gone is counted and named, not silently missing (ADR-017, section 9; SPEC §13).
func TestLoad_TheIndexLoadsAgainFromTheLoadList(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	r.Write(t, "rdf/a.ttl", turtle)
	r.Write(t, "rdf/b.ttl", "<https://example.org/x> <https://example.org/y> \"z\" .\n")
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/a.ttl", "rdf/b.ttl"}}); err != nil {
		t.Fatal(err)
	}
	d, err := oxigraph.Ensure(ctx, r.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	q := AskRequest{Query: "sparql", Sparql: `SELECT ?g WHERE { GRAPH ?g { ?s ?p ?o } } GROUP BY ?g`}
	a, err := c.Ask(ctx, q)
	if err != nil || len(a.Sparql.Rows) != 2 || a.Sparql.LoadMissing != 0 {
		t.Fatalf("after a clear, both graphs are loaded again: %+v %v", a, err)
	}
	must(t, os.Remove(filepath.Join(r.Root, "rdf/b.ttl")))
	must(t, d.Clear(ctx))
	a, err = c.Ask(ctx, q)
	if err != nil || len(a.Sparql.Rows) != 1 || a.Sparql.LoadMissing != 1 {
		t.Errorf("a file that is gone: want one graph and one missing, got %+v %v", a.Sparql, err)
	}
	if l, _ := c.LoadedGraphs(ctx); len(l.Missing) != 1 || l.Missing[0].From != "rdf/b.ttl" {
		t.Errorf("the list names the missing file: %+v", l)
	}
}

// A file that asserts its own producer stays hash only: its triples are in its own graph, never in the
// view a check for the producing foton reads (ADR-017, section 3).
func TestLoad_AFileCannotNameItsOwnProducer(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	// A file cannot name its own hash (the hash covers the line), so it names a producer for another
	// file IRI; what matters is that such triples stay in the file's own graph.
	claim := "<https://kton.dev/o/" + strings.Repeat("0", 64) + "> <http://www.w3.org/ns/prov#wasGeneratedBy> <https://example.org/fake-foton> .\n"
	r.Write(t, "rdf/self.nt", claim)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/self.nt"}}); err != nil {
		t.Fatal(err)
	}
	a, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: `PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?f WHERE { ?file prov:wasGeneratedBy ?f }`})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range a.Sparql.Rows {
		if row["f"] == "https://example.org/fake-foton" {
			t.Errorf("a loaded file's own prov:wasGeneratedBy reached the view: %+v", a.Sparql.Rows)
		}
	}
}

// Two repositories on one machine load the same file: one graph, listed in both catalogues. A drop
// takes it out of the dropping repository's queries and deletes it only when no catalogue lists it; each
// repository sees only its own catalogue (ADR-017, sections 2, 5 and 8).
func TestLoad_AGraphTwoRepositoriesLoadedStaysUntilBothDropIt(t *testing.T) {
	a := loadingRepo(t) // sets COCKPIT_INDEX_DIR; b shares the index
	b := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = "oxigraph"
	raw.Index.Derived.Enabled = true
	b.WriteConfig(t, raw)
	ctx := context.Background()
	a.Write(t, "rdf/x.ttl", turtle)
	b.Write(t, "rdf/x.ttl", turtle)
	hash := core.HashBytes([]byte(turtle))
	ca, cb := New(Start{Dir: a.Root}), New(Start{Dir: b.Root})
	t.Setenv("COCKPIT_REPO_DIR", "") // each cockpit by its own Start.Dir
	graphs := func(c *Cockpit) int {
		t.Helper()
		ans, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`})
		if err != nil {
			t.Fatal(err)
		}
		return len(ans.Sparql.Rows)
	}
	if _, err := ca.Load(ctx, LoadRequest{Files: []string{"rdf/x.ttl"}}); err != nil {
		t.Fatal(err)
	}
	if n := graphs(cb); n != 0 {
		t.Fatalf("a repository sees another's loads: %d", n)
	}
	if _, err := cb.Load(ctx, LoadRequest{Files: []string{"rdf/x.ttl"}}); err != nil {
		t.Fatal(err)
	}
	if err := ca.DropLoaded(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if graphs(ca) != 0 || graphs(cb) != 1 {
		t.Errorf("after a's drop: a sees %d, b sees %d; want 0 and 1", graphs(ca), graphs(cb))
	}
	if err := cb.DropLoaded(ctx, hash); err != nil {
		t.Fatal(err)
	}
	d, err := oxigraph.Ensure(ctx, b.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	if l, err := d.Loaded(ctx, b.Root); err != nil || len(l) != 0 {
		t.Errorf("after both drops the catalogue still lists it: %+v %v", l, err)
	}
}
