//go:build docker

package cockpit

import (
	"context"
	"strings"
	"testing"

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
