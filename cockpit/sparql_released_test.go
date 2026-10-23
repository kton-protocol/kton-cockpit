//go:build !unreleased

package cockpit

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The projection a free query reads is not in a released kernel, so a released cockpit says so
// rather than answering nothing.
func TestSparql_AReleasedCockpitSaysItCannotAnswer(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, err := New(Start{}).Ask(context.Background(), AskRequest{Query: "sparql", Sparql: `ASK { ?s ?p ?o }`})
	if got := refusalCode(t, err); got != "sparql.unavailable" {
		t.Fatalf("refused as %s, want sparql.unavailable", got)
	}
}
