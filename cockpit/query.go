//go:build unreleased

package cockpit

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// Search and profile rules are SPARQL over the registries' RDF projection (kton K8), evaluated in a
// pinned Oxigraph image. What a query reads is oxigraph.Dataset, on the daemon and in a container
// alike: every plankton record of the union, attributed only where a key of this repository's trust
// tiers verifies; claims only when such a key signed them. A query that wants trusted records only
// filters on prov:wasAttributedTo.

// queryAnswer is a vocabulary query's rows, the package the query came from, and how it was read.
type queryAnswer struct {
	rows []map[string]string
	from string
	// via says where the answer was evaluated: "" for a container for this query alone, or the
	// index daemon and how many registries it read (ADR-012), or why the daemon was not used.
	via string
}

// runQuery evaluates the named query from the allowed vocabulary and returns its rows.
func runQuery(ctx context.Context, cfg *config.Config, name string) (*queryAnswer, error) {
	if cfg.Raw.Query.Image == "" && cfg.Raw.Index.URL == "" {
		return nil, refuse("query.not-configured", "", "no SPARQL engine configured (query.image in cockpit.config.json)")
	}
	voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return nil, refuse("vocabulary", "", "%v", err)
	}
	q, ok := voc.Queries[name]
	if !ok {
		return nil, refuse("query.unknown", "", "no allowed package brings the query %q — install a vocabulary package and name it in claims.allowedPackages", name)
	}
	src := voc.From["query:"+name]
	from := src.Name + " (" + src.Dir + ")"
	// Refused on either path: in the shared daemon SERVICE would leave this query's dataset
	// (SPEC §12.4), and a query must not answer differently by where it runs.
	if err := index.CheckQuery(string(q)); err != nil {
		return nil, refuse("query.refused", "", "%s (from %s): %v", name, from, err)
	}

	keys, err := trustedKeys(cfg)
	if err != nil {
		return nil, refuse("trust", "", "%v", err)
	}
	via := ""
	if cfg.Raw.Index.Kind == "oxigraph" {
		d, err := oxigraph.Ensure(ctx, cfg)
		if err == nil {
			a, err := oxigraph.Query(ctx, d, cfg, keys, string(q))
			if err != nil {
				return nil, refuse("query.failed", "", "%v", err)
			}
			kept := "read into it for this query"
			if a.Kept {
				kept = "current"
			}
			return &queryAnswer{rows: shortIDs(a.Rows), from: from,
				via: fmt.Sprintf("index oxigraph, %d registries (%s)", a.Registries, kept)}, nil
		}
		if cfg.Raw.Query.Image == "" {
			return nil, refuse("query.failed", "", "%v", err)
		}
		// A daemon this configuration may not use is reported, and the query is answered the way
		// it was before the daemon existed.
		via = "not the index: " + err.Error()
	}
	res, err := inContainer(ctx, cfg, keys, q, "csv", 0)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(res)).ReadAll()
	if err != nil {
		return nil, refuse("query.failed", "", "reading the result: %v", err)
	}
	var out []map[string]string
	for i, row := range rows {
		if i == 0 {
			continue
		}
		m := map[string]string{}
		for j, v := range row {
			if j < len(rows[0]) {
				m[rows[0][j]] = v
			}
		}
		out = append(out, m)
	}
	return &queryAnswer{rows: shortIDs(out), from: from, via: via}, nil
}

// shortIDs writes a content node (https://kton.dev/o/<hex>) as the id it is, sha256:<hex>.
func shortIDs(rows []map[string]string) []map[string]string {
	for _, row := range rows {
		for k, v := range row {
			row[k] = strings.Replace(v, "https://kton.dev/o/", "sha256:", 1)
		}
	}
	return rows
}
