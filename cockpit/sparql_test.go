package cockpit

import (
	"context"
	"errors"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return r.Code
}

// A free query narrows itself: a ref or a filter beside it would read as narrowing and do nothing,
// so each is refused before anything runs, and so is a query that is not one the cockpit answers.
func TestSparql_RefusesWhatItWouldNotAnswer(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	c := New(Start{})
	sel := `SELECT * { ?s ?p ?o }`
	for _, tc := range []struct {
		in   AskRequest
		code string
	}{
		{AskRequest{Query: "sparql"}, "argument"},
		{AskRequest{Query: "sparql", Sparql: sel, Ref: "sha256:00"}, "argument"},
		{AskRequest{Query: "sparql", Sparql: sel, Filter: &AskFilter{TrustTier: "self"}}, "filter.invalid"},
		{AskRequest{Query: "sparql", Sparql: sel, Limit: sparqlMaxLimit + 1}, "argument"},
		{AskRequest{Query: "sparql", Sparql: sel, TimeoutSeconds: -1}, "argument"},
	} {
		_, err := c.Ask(context.Background(), tc.in)
		if got := refusalCode(t, err); got != tc.code {
			t.Errorf("%+v: refused as %s, want %s", tc.in, got, tc.code)
		}
	}
}
