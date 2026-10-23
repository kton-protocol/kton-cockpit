//go:build unreleased

package cockpit

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A query is checked before an engine is looked for, and without one nothing is answered.
func TestSparql_ChecksTheQueryAndNeedsAnEngine(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	c := New(Start{})
	_, err := c.Ask(context.Background(), AskRequest{Query: "sparql", Sparql: `ASK { ?s ?p ?o }`})
	if got := refusalCode(t, err); got != "query.not-configured" {
		t.Fatalf("no engine: refused as %s, want query.not-configured", got)
	}
	raw := r.Config(t).Raw
	raw.Query.Image = "oci://ghcr.io/oxigraph/oxigraph@sha256:" + "00000000000000000000000000000000000000000000000000000000000000aa"
	r.WriteConfig(t, raw)
	for _, q := range []string{
		`CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }`,
		`SELECT * FROM <https://kton.dev/o/00> { ?s ?p ?o }`,
		`SELECT * { SERVICE <http://127.0.0.1:7878/query> { ?s ?p ?o } }`,
	} {
		_, err := c.Ask(context.Background(), AskRequest{Query: "sparql", Sparql: q})
		if got := refusalCode(t, err); got != "query.refused" {
			t.Errorf("%s: refused as %s, want query.refused", q, got)
		}
	}
}
