//go:build unreleased

package oxigraph_test

import (
	"fmt"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// A SELECT keeps its variables in the query's order, an unbound one is absent from its row, and an
// ASK is its boolean.
func TestParseResults_ReadsASelectAndAnAsk(t *testing.T) {
	sel, err := oxigraph.ParseResults([]byte(`{"head":{"vars":["step","signer"]},"results":{"bindings":[
		{"step":{"type":"uri","value":"https://kton.dev/o/aa"},"signer":{"type":"uri","value":"https://kton.dev/agent/01"}},
		{"step":{"type":"uri","value":"https://kton.dev/o/bb"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sel.Vars) != "[step signer]" || len(sel.Rows) != 2 || sel.Rows[0]["signer"] != "https://kton.dev/agent/01" ||
		sel.Rows[1]["step"] != "https://kton.dev/o/bb" || sel.Boolean != nil {
		t.Fatalf("select read as %+v", sel)
	}
	if _, ok := sel.Rows[1]["signer"]; ok {
		t.Errorf("an unbound variable was given a value: %v", sel.Rows[1])
	}
	ask, err := oxigraph.ParseResults([]byte(`{"head":{},"boolean":true}`))
	if err != nil || ask.Boolean == nil || !*ask.Boolean || len(ask.Rows) != 0 || ask.Vars == nil {
		t.Fatalf("ask read as %+v, %v", ask, err)
	}
	if _, err := oxigraph.ParseResults([]byte(`a,b`)); err == nil {
		t.Error("CSV was read as JSON results")
	}
}
