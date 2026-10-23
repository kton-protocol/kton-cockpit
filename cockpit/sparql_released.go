//go:build !unreleased

package cockpit

import (
	"context"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The RDF projection a query reads (kton K8, plankton/rdf and nekton/nanopub) is not in a released
// kernel yet. This build is against released modules only, so query sparql refuses.
func runSparql(context.Context, *config.Config, string, time.Duration) (*SparqlAnswer, error) {
	return nil, refuse("sparql.unavailable", "SPEC §9.9", "query sparql reads the kernel's RDF projection, which is not in a released kton yet; this cockpit is built without it (build with -tags unreleased)")
}
