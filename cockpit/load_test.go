//go:build docker

package cockpit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
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

// A load whose graph name would be a record's is refused: a file whose bytes hash to a record id of the
// union (here a claim's payload, whose hash is its id), and a load with no triples (kton-review L1 on
// #192).
func TestLoad_AFileThatHashesToARecordIsRefused(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	said, err := c.Say(ctx, SayRequest{Subject: "urn:example:x", Template: "working-on",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := binaries.New(r.Config(t)).EnvelopeFor(ctx, said.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	var env core.Envelope
	must(t, json.Unmarshal(raw, &env))
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	must(t, err)
	if core.HashBytes(payload) != said.ClaimID {
		t.Fatalf("the payload does not hash to the claim id; the test cannot build its file")
	}
	r.Write(t, "rdf/record.jsonld", string(payload))
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/record.jsonld"}}); codeOf(err) != "derived.clash" {
		t.Errorf("a file with a record's hash: want derived.clash, got %v", err)
	}
	r.Write(t, "rdf/empty.jsonld", "{}")
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/empty.jsonld"}}); codeOf(err) != "derived.refused" {
		t.Errorf("a load with no triples: want derived.refused, got %v", err)
	}
}

// A path is checked where it leads: a symlink out of the repository, or into .kton/local, is refused,
// and so is a signing key (kton-review L2, T25 on #192).
func TestLoad_APathIsCheckedWhereItLeads(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	outside := filepath.Join(t.TempDir(), "outside.ttl")
	must(t, os.WriteFile(outside, []byte(turtle), 0o644))
	must(t, os.MkdirAll(filepath.Join(r.Root, "rdf"), 0o755))
	must(t, os.Symlink(outside, filepath.Join(r.Root, "rdf", "out.ttl")))
	must(t, os.MkdirAll(filepath.Join(r.Root, ".kton", "local"), 0o755))
	must(t, os.WriteFile(filepath.Join(r.Root, ".kton", "local", "x.ttl"), []byte(turtle), 0o644))
	must(t, os.Symlink(filepath.Join(r.Root, ".kton", "local", "x.ttl"), filepath.Join(r.Root, "rdf", "local.ttl")))
	r.Write(t, "rdf/k.key", turtle)
	for _, f := range []string{"rdf/out.ttl", "rdf/local.ttl", "rdf/k.key"} {
		if _, err := c.Load(ctx, LoadRequest{Files: []string{f}, As: "text/turtle"}); codeOf(err) != "path.denied" {
			t.Errorf("%s: want path.denied, got %v", f, err)
		}
	}
}

// index.derived.maxTriples bounds what one repository has loaded; a file that changed under its path
// is not loaded again after a rebuild, and is counted (kton-review T25 on #192).
func TestLoad_TheTripleLimitAndAChangedFile(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	raw := r.Config(t).Raw
	raw.Index.Derived.MaxTriples = 3
	r.Write(t, "rdf/two.ttl", turtle)
	r.Write(t, "rdf/also-two.ttl", "<https://example.org/a> <https://example.org/b> 1, 2 .\n")
	r.WriteConfig(t, raw)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/two.ttl"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/also-two.ttl"}}); codeOf(err) != "derived.refused" {
		t.Errorf("past maxTriples: want derived.refused, got %v", err)
	}

	r.Write(t, "rdf/two.ttl", "<https://example.org/changed> <https://example.org/p> 1 .\n")
	d, err := oxigraph.Ensure(ctx, r.Config(t))
	must(t, err)
	must(t, d.Clear(ctx))
	a, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: `SELECT ?s WHERE { GRAPH ?g { ?s ?p ?o } }`})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sparql.Rows) != 0 || a.Sparql.LoadMissing != 1 {
		t.Errorf("a file that changed is not loaded again, and is counted: %+v", a.Sparql)
	}
}

// A loaded graph is described where a claims query finds it (ADR-018, section 4a): a nk:LoadedGraph
// with the output path its producing step records, the label the load gave it, and when it was loaded;
// only this repository's loads, listed per request, never another repository's (kton-review B1).
func TestLoad_ALoadedGraphIsDescribedForAQuery(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "make.sh", "printf '%s\\n' '"+strings.TrimSpace(turtle)+"' > rdf/sidefx.ttl\n")
	r.Write(t, "rdf/sidefx.ttl", turtle)
	if res, _, err := Publish(ctx, nil, PublishRequest{Inputs: []string{"data/in.csv", "make.sh"},
		Outputs: []string{"rdf/sidefx.ttl"}, Cmd: "sh make.sh"}); err != nil || res.IsError {
		t.Fatalf("publish: %v %s", err, errText(res))
	}
	if _, err := New(Start{}).Load(ctx, LoadRequest{Files: []string{"rdf/sidefx.ttl"}, Label: "sidefx"}); err != nil {
		t.Fatalf("load: %v", err)
	}
	describedIn := func(t *testing.T) []map[string]string {
		t.Helper()
		return sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?g ?path ?label ?when WHERE { ?g a nk:LoadedGraph ; nk:loadedAt ?when .
  OPTIONAL { ?g nk:path ?path } OPTIONAL { ?g nk:loadedAs ?label } }`).Rows
	}
	rows := describedIn(t)
	hex := strings.TrimPrefix(core.HashBytes([]byte(turtle)), "sha256:")
	if len(rows) != 1 || rows[0]["g"] != "https://kton.dev/o/"+hex || rows[0]["path"] != "rdf/sidefx.ttl" ||
		rows[0]["label"] != "sidefx" || rows[0]["when"] == "" {
		t.Errorf("the loaded graph's description: %v", rows)
	}

	// The bookkeeping of the catalogue is no query's (kton-review on #208).
	if a := sparqlAsk(t, `ASK { ?g <urn:kton:loaded#from> ?f }`); a.Boolean == nil || *a.Boolean {
		t.Error("the catalogue's bookkeeping must not be in a query's dataset")
	}

	// A rebuilt index loads the file again from the load list, label and all (kton-review T31 on #208).
	d, err := oxigraph.Ensure(ctx, r.Config(t))
	must(t, err)
	must(t, d.Clear(ctx))
	if rows := describedIn(t); len(rows) != 1 || rows[0]["label"] != "sidefx" || rows[0]["path"] != "rdf/sidefx.ttl" {
		t.Errorf("after a rebuild, the description again: %v", rows)
	}

	// Another repository on the same machine sees its own loads only.
	other := loadingRepo(t)
	other.Write(t, "rdf/other.ttl", "<https://example.org/a> <https://example.org/b> \"c\" .\n")
	if _, err := New(Start{}).Load(ctx, LoadRequest{Files: []string{"rdf/other.ttl"}}); err != nil {
		t.Fatalf("load in the other repository: %v", err)
	}
	if rows := describedIn(t); len(rows) != 1 || rows[0]["path"] != "" || rows[0]["label"] != "" {
		t.Errorf("the other repository sees its one load, undescribed: %v", rows)
	}
}

// A step nobody this repository trusts signed names no path of its loaded graph, though it wrote the same
// bytes (kton-review F1 on #208).
func TestLoad_AnUntrustedStepNamesNoPath(t *testing.T) {
	ctx := context.Background()
	theirs := testrepo.New(t)
	theirs.Use(t)
	theirs.Write(t, "data/in.csv", "id\n1\n")
	theirs.Write(t, "rdf/elsewhere.ttl", turtle)
	if res, _, err := Publish(ctx, nil, PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"rdf/elsewhere.ttl"},
		Cmd: "cp data/in.csv /dev/null"}); err != nil || res.IsError {
		t.Fatalf("their publish: %v %s", err, errText(res))
	}

	r := loadingRepo(t)
	raw := r.Config(t).Raw
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	must(t, json.Unmarshal(b, &spec))
	raw.Federation.Sources = []source.Spec{spec} // their key is in no tier
	r.WriteConfig(t, raw)
	r.Write(t, "rdf/coverage.ttl", turtle)
	if _, err := New(Start{}).Load(ctx, LoadRequest{Files: []string{"rdf/coverage.ttl"}}); err != nil {
		t.Fatalf("load: %v", err)
	}
	a := sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?path WHERE { ?g a nk:LoadedGraph ; nk:path ?path }`)
	if len(a.Rows) != 0 {
		t.Errorf("only a trusted step names a path; theirs is not trusted: %v", a.Rows)
	}
}

// An index of the repository's own (COCKPIT_INDEX_DIR) is not bound by the shared index's limit.
func TestLoad_AnOwnIndexIsNotBoundByTheSharedLimit(t *testing.T) {
	r := loadingRepo(t)
	raw := r.Config(t).Raw
	limit := int64(8)
	raw.Index.Derived.MaxBytesShared = &limit
	r.WriteConfig(t, raw)
	r.Write(t, "rdf/a.ttl", turtle)
	if _, err := New(Start{}).Load(context.Background(), LoadRequest{Files: []string{"rdf/a.ttl"}}); err != nil {
		t.Errorf("an own index: want the load, got %v", err)
	}
}
