package cli

import (
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// `cockpit ask sparql` reads its query from a file and goes to the same handler as the JSON form:
// without an engine configured it refuses (exit 2) and says why.
func TestShellSparql_ReadsTheQueryFromAFile(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "q.rq", "ASK { ?s ?p ?o }\n")

	out, code := r.Cockpit(t, "ask", "sparql")
	if code != 1 || !strings.Contains(out, "usage: cockpit ask sparql") {
		t.Errorf("no query file: want usage and exit 1, got %d:\n%s", code, out)
	}
	out, code = r.Cockpit(t, "ask", "sparql", "q.rq", "--limit", "ten")
	if code != 1 || !strings.Contains(out, "--limit takes a number") {
		t.Errorf("a bad limit: want exit 1, got %d:\n%s", code, out)
	}
	out, code = r.Cockpit(t, "ask", "sparql", "q.rq")
	if code != 2 || !strings.Contains(out, "no SPARQL engine configured") {
		t.Errorf("no engine: want the refusal and exit 2, got %d:\n%s", code, out)
	}
	if out, _ := r.Cockpit(t, "ask"); !strings.Contains(out, "sparql") {
		t.Errorf("ask does not list sparql:\n%s", out)
	}
}
