//go:build docker

package oxigraph_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A view is replaced graph by graph: after one claim in one scope, the rebuild uploads that scope's graph
// and the rest only, and the view answers as a whole rebuild would (MH, 21.11.: a minute of blank after
// signing).
func TestOxigraph_ARebuildReplacesOnlyTheGraphsThatChanged(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	scopes := map[string]string{}
	for _, s := range []string{"a", "b", "c"} {
		scopes[s] = r.SeedScope(t, s)
	}
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = scopes
	raw.Query.Image = image
	raw.Index.Kind = "oxigraph"
	r.WriteConfig(t, raw)
	r.Use(t)
	cfg := r.Config(t)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	say := func(scope, step string) {
		t.Helper()
		if _, err := cockpit.New(cockpit.Start{}).Say(ctx, cockpit.SayRequest{Subject: "urn:example:x", Template: "working-on", Scope: scope,
			Fields: map[string]string{"step": step, "by-session": testrepo.SessionID}}); err != nil {
			t.Fatalf("say: %v", err)
		}
	}
	for i, s := range []string{"a", "b", "c", "a", "b", "c"} {
		say(s, fmt.Sprint(i))
	}
	count := func(q string) string {
		t.Helper()
		a, err := oxigraph.Free(ctx, d, cfg, keys(t, cfg), q, oxigraph.DatasetClaims, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return a.Rows[0]["n"]
	}
	claims := `SELECT (COUNT(DISTINCT ?c) AS ?n) WHERE { ?c <https://kton.dev/v/predicate> <https://kton.dev/v/working-on> }`
	inScope := func(s string) string {
		return `SELECT (COUNT(DISTINCT ?c) AS ?n) WHERE { ?g <https://kton.dev/v/scope> <https://kton.dev/o/` + scopes[s][len("sha256:"):] + `> .
  GRAPH ?g { ?c <https://kton.dev/v/predicate> <https://kton.dev/v/working-on> } }`
	}
	if n := count(claims); n != "6" {
		t.Fatalf("the first view: want 6 claims, got %s", n)
	}
	// A scope graph holds only its own claims' triples (a claim node, or a claim's nested node): what a
	// claim asserts about something else (a seed's name for its scope) is in no scope graph, so no two
	// scope graphs share a triple (kton-review N1 on #226).
	if n := count(`SELECT (COUNT(*) AS ?n) WHERE { ?g a <https://kton.dev/v/ClaimGraph> . GRAPH ?g { ?s ?p ?o }
  FILTER(!STRSTARTS(STR(?s), "https://kton.dev/o/") && !STRSTARTS(STR(?s), "https://kton.dev/np/")) }`); n != "0" {
		t.Errorf("triples about something other than a claim in the scope graphs: %s", n)
	}
	before := oxigraph.Staged()
	say("b", "new")
	if n := count(claims); n != "7" {
		t.Errorf("after one claim in b: want 7 claims in the union, got %s", n)
	}
	if up := oxigraph.Staged() - before; up != 2 {
		t.Errorf("one claim in b: want b's graph and the rest uploaded, got %d graphs", up)
	}
	for s, want := range map[string]string{"a": "2", "b": "3", "c": "2"} {
		if n := count(inScope(s)); n != want {
			t.Errorf("scope %s: want %s claims, got %s", s, want, n)
		}
	}

	// A field renamed by an alias changes every graph: what the old graphs held leaves the union, so no
	// claim keeps its old term beside the new one.
	r.Write(t, ".kton/aliases.json", `{"prefixes":{"q":"https://example.org/q/"},"terms":{"step":"q:step"}}`)
	if n := count(`SELECT (COUNT(*) AS ?n) WHERE { ?c <https://kton.dev/v/lab/step> ?v }`); n != "0" {
		t.Errorf("after the rename, no lab:step left in the union: %s", n)
	}
	if n := count(`SELECT (COUNT(*) AS ?n) WHERE { ?c <https://example.org/q/step> ?v }`); n != "7" {
		t.Errorf("after the rename, every claim's q:step: %s", n)
	}
}
