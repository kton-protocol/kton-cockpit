//go:build docker

package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// Each scope's claims are also in a named graph of their own, described in the default graph; the
// unscoped claims in one more; and the default graph still holds every claim (ADR-018).
func TestScopeGraphs_EachScopesClaimsInAGraphOfItsOwn(t *testing.T) {
	// The same answers wherever the query is evaluated (SPEC §9.9): a container for the query, or the index.
	for _, kind := range []string{"", "oxigraph"} {
		t.Run("index="+kind, func(t *testing.T) { scopeGraphsIn(t, kind) })
	}
}

func scopeGraphsIn(t *testing.T, kind string) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	ctx := context.Background()
	r := testrepo.New(t)
	parent := r.SeedScope(t, "programme")
	study := r.SeedScopeUnder(t, "study", parent)
	review := r.SeedScope(t, "review")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study": study, "review": review}
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = kind
	r.WriteConfig(t, raw)
	r.Use(t)
	if kind == "oxigraph" {
		t.Cleanup(func() { oxigraph.Stop(context.Background(), r.Config(t)) })
	}
	step := publishOne(t, r).FotonID
	say := func(scope, value string) string {
		t.Helper()
		res, out, err := Say(ctx, nil, SayRequest{Subject: step, Template: "describes", Scope: scope,
			Fields: map[string]string{"value": value}})
		if err != nil || res.IsError {
			t.Fatalf("say in %q: %v %s", scope, err, errText(res))
		}
		return "https://kton.dev/o/" + hexOf(out.ClaimID)
	}
	inStudy := say("study", "one")
	inStudy2 := say("study", "two")
	inReview := say("review", "three")
	unscoped := say("", "four")
	seed := func(id string) string { return "https://kton.dev/o/" + hexOf(id) }

	// The default graph: every claim, as before.
	all := column(sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?c WHERE { ?c a nk:Claim ; nk:predicate <http://purl.org/dc/terms/description> }`), "c")
	for _, c := range []string{inStudy, inStudy2, inReview, unscoped} {
		if !all[c] {
			t.Errorf("the default graph lost %s: %v", c, all)
		}
	}

	// Found by the seed, exactly; read with GRAPH.
	a := sparqlAsk(t, `ASK { ?s ?p ?o }`)
	if index := strings.HasPrefix(a.Via, "index oxigraph"); index != (kind == "oxigraph") {
		t.Fatalf("index.kind %q answered via %q", kind, a.Via)
	}
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?c WHERE { ?g a nk:ClaimGraph ; nk:scope <`+seed(study)+`> . GRAPH ?g { ?c nk:about ?s } }`)
	if got := column(a, "c"); len(got) != 2 || !got[inStudy] || !got[inStudy2] {
		t.Errorf("the study's graph: want its two claims, got %v", a.Rows)
	}

	// The metadata: name, parent and count; the unscoped graph marked.
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?label ?parent ?n WHERE { ?g a nk:ClaimGraph ; nk:scope <`+seed(study)+`> ; rdfs:label ?label ; nk:parent ?parent ; nk:claims ?n }`)
	if len(a.Rows) != 1 || a.Rows[0]["label"] != "study" || a.Rows[0]["parent"] != seed(parent) || a.Rows[0]["n"] != "2" {
		t.Errorf("the study graph's metadata: %v", a.Rows)
	}
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?c WHERE { ?g a nk:ClaimGraph ; nk:unscoped true . GRAPH ?g { ?c nk:about ?s } }`)
	if got := column(a, "c"); !got[unscoped] || got[inStudy] || got[inReview] {
		t.Errorf("the unscoped graph: want the unscoped claim and no scoped one, got %v", a.Rows)
	}

	// A claim is in its scope's graph only.
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?g WHERE { GRAPH ?g { <`+inReview+`> nk:about ?s } }`)
	if len(a.Rows) != 1 {
		t.Errorf("the review claim must be in one graph: %v", a.Rows)
	}

	// A new claim rebuilds the view: the graphs follow, none left behind.
	say("study", "five")
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?n WHERE { ?g a nk:ClaimGraph ; nk:scope <`+seed(study)+`> ; nk:claims ?n }`)
	if len(a.Rows) != 1 || a.Rows[0]["n"] != "3" {
		t.Errorf("after one more claim, one study graph of three: %v", a.Rows)
	}
}

// A rebuilt view replaces its scope graphs: after an alias change (the same slot, another view), a
// scope's graph holds the field under its new term only, not the old one beside it (kton-review T28 on
// #205).
func TestScopeGraphs_ARebuiltViewReplacesItsScopeGraphs(t *testing.T) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	ctx := context.Background()
	r := testrepo.New(t)
	study := r.SeedScope(t, "study")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study": study}
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = "oxigraph"
	r.WriteConfig(t, raw)
	r.Use(t)
	t.Cleanup(func() { oxigraph.Stop(context.Background(), r.Config(t)) })
	step := publishOne(t, r).FotonID
	if res, _, err := Say(ctx, nil, SayRequest{Subject: step, Template: "describes", Scope: "study",
		Fields: map[string]string{"value": "one"}}); err != nil || res.IsError {
		t.Fatalf("say: %v %s", err, errText(res))
	}
	terms := func() map[string]bool {
		t.Helper()
		return column(sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
SELECT ?p WHERE { ?g a nk:ClaimGraph ; nk:scope <https://kton.dev/o/`+hexOf(study)+`> .
  GRAPH ?g { ?c nk:about ?s ; ?p "one" } }`), "p")
	}
	if got := terms(); !got["https://kton.dev/v/lab/value"] {
		t.Fatalf("before the alias: want lab:value, got %v", got)
	}
	r.Write(t, ".kton/aliases.json", `{"prefixes":{"q":"https://example.org/q/"},"terms":{"value":"q:value"}}`)
	got := terms()
	if !got["https://example.org/q/value"] || got["https://kton.dev/v/lab/value"] {
		t.Errorf("after the alias: want q:value alone in the scope's graph, got %v", got)
	}
}

// A scope graph names the source its seed came from: "own" for the repository's, the source's name
// for a federated one; both engines alike (ADR-018, section 3).
func TestScopeGraphs_AGraphNamesItsSource(t *testing.T) {
	for _, kind := range []string{"", "oxigraph"} {
		t.Run("index="+kind, func(t *testing.T) { scopeSourcesIn(t, kind) })
	}
}

func scopeSourcesIn(t *testing.T, kind string) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	ctx := context.Background()
	say := func(subject, scope, value string) {
		t.Helper()
		if res, _, err := Say(ctx, nil, SayRequest{Subject: subject, Template: "describes", Scope: scope,
			Fields: map[string]string{"value": value}}); err != nil || res.IsError {
			t.Fatalf("say in %q: %v %s", scope, err, errText(res))
		}
	}

	theirs := testrepo.New(t)
	runs := theirs.SeedScope(t, "runs")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"runs": runs}
	theirs.WriteConfig(t, raw)
	theirs.Use(t)
	say(publishOne(t, theirs).FotonID, "runs", "theirs")

	ours := testrepo.New(t)
	study := ours.SeedScope(t, "study")
	raw = testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study": study}
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	must(t, json.Unmarshal(b, &spec))
	raw.Federation.Sources = []source.Spec{spec}
	key, err := os.ReadFile(filepath.Join(theirs.Root, ".kton", "keys", "self", testrepo.SessionID+"-claims.pub"))
	must(t, err)
	ours.Write(t, ".kton/keys/peer/theirs.pub", string(key))
	raw.Trust.Tiers = append(raw.Trust.Tiers, "peer")
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = kind
	ours.WriteConfig(t, raw)
	ours.Use(t)
	if kind == "oxigraph" {
		t.Cleanup(func() { oxigraph.Stop(context.Background(), ours.Config(t)) })
	}
	say(publishOne(t, ours).FotonID, "study", "ours")

	a := sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?label ?source WHERE { ?g a nk:ClaimGraph ; rdfs:label ?label ; nk:source ?source }`)
	got := map[string]string{}
	for _, row := range a.Rows {
		got[row["label"]] += row["source"]
	}
	// Each repository's root scope has a graph too, from its own source.
	if got["study"] != "own" || got["runs"] != "theirs" {
		t.Errorf("want study from own, runs from theirs: %v", a.Rows)
	}

	// The answer lists the claim graphs it read, with their source (ADR-018, section 3).
	listed := map[string]string{}
	for _, g := range a.ClaimGraphs {
		listed[g.Label] = strings.Join(g.Source, ",")
	}
	if listed["study"] != "own" || listed["runs"] != "theirs" {
		t.Errorf("the answer's claim graphs: %+v", a.ClaimGraphs)
	}
	if len(a.RepeatedLabels) != 0 {
		t.Errorf("no label repeats here: %v", a.RepeatedLabels)
	}

	// A second scope named "runs", this repository's own: the name now repeats, and reading by name
	// reads both graphs.
	runs2 := ours.SeedScope(t, "runs")
	raw.Claims.Scopes["runs"] = runs2
	ours.WriteConfig(t, raw)
	say(publishOne(t, ours).FotonID, "runs", "ours too")
	a = sparqlAsk(t, `PREFIX nk: <https://kton.dev/v/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?c WHERE { ?g a nk:ClaimGraph ; rdfs:label "runs" . GRAPH ?g { ?c nk:about ?s } }`)
	if len(a.Rows) != 2 || strings.Join(a.RepeatedLabels, ",") != "runs" {
		t.Errorf("two scopes named runs: want both graphs read and the label named, got %d rows, repeated %v", len(a.Rows), a.RepeatedLabels)
	}
}
