//go:build docker

package cockpit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A query answered in a container of its own reads its dataset from under the checkout's local
// directory, a path the engine's host has too (the persona windows reach the engine through a socket
// passed in from the host), and leaves nothing there (userinterface, 21.11.).
func TestSparql_AQueryContainerReadsFromTheCheckout(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)
	a := sparqlAsk(t, `ASK { ?s ?p ?o }`)
	if a.Boolean == nil {
		t.Fatalf("the container answered nothing: %+v", a)
	}
	entries, err := os.ReadDir(filepath.Join(r.Root, ".kton", "local", "tmp"))
	if err != nil {
		t.Fatalf("the query's directory is under .kton/local/tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("nothing left after the query: %v", entries)
	}
}
