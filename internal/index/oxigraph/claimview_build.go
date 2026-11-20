package oxigraph

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// ViewConfigFor is what cfg adds to the kernel's projection: the name and tier of each trusted key,
// and the repository's templates.
func ViewConfigFor(cfg *config.Config) (ViewConfig, error) {
	vc := ViewConfig{Signers: map[string]Signer{}}
	for path, tier := range cfg.TierPubkeys() {
		b, err := os.ReadFile(path)
		if err != nil {
			return vc, fmt.Errorf("trust tier %q names %s, which cannot be read: %w", tier, path, err)
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
		if err != nil {
			return vc, fmt.Errorf("trust tier %q names %s, which is not a public key: %w", tier, path, err)
		}
		vc.Signers[core.KeyIDHex(pub)] = Signer{Name: strings.TrimSuffix(filepath.Base(path), ".pub"), Tier: tier}
	}
	set, own, err := binaries.New(cfg).TemplatesAndAliases()
	if err != nil {
		return vc, err
	}
	vc.Templates, vc.Aliases = set, own
	if vc.Rows, err = binaries.New(cfg).RowTemplates(); err != nil {
		return vc, err
	}
	for _, n := range set.Names() {
		t, _ := set.Get(n)
		for field := range t.Fields {
			if r := vc.resolve(n, field); Reserved(r) {
				return vc, fmt.Errorf("template %s: field %q is aliased to %s, a term the query projection writes itself", n, field, r)
			}
		}
	}
	vc.Templates = set
	return vc, nil
}

// ClaimDataset is the query projection of the claims for a container started for one query: the
// same ClaimView the daemon builds, from the same kernel projection, as N-Triples.
func ClaimDataset(u *binaries.Union, keys []ed25519.PublicKey, vc ViewConfig, sources map[string][]string) (claims, scopes []byte, graphs []ClaimGraph, err error) {
	var trusted, structure []Quad
	for _, p := range projectNekton(u.Nekton.Records(0), keys) {
		own, err := ParseNQuads(p.own)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, q := range own {
			if isClaimGraph(q.G, npPub) {
				structure = append(structure, q)
			}
		}
		if len(p.attr) == 0 {
			continue
		}
		trusted = append(trusted, own...)
		for _, a := range p.attr {
			qs, err := ParseNQuads(a)
			if err != nil {
				return nil, nil, nil, err
			}
			trusted = append(trusted, qs...)
		}
	}
	// The scope graphs as a container loads them (ADR-018): named graphs beside the default graph,
	// under a slot of their own; a query finds them by their metadata, not by their names.
	view := ClaimView(trusted, structure, vc)
	split, meta := scopeGraphs("query", view, sources)
	return NTriples(append(view, meta...)), NQuads(split), describeGraphs(meta), nil
}

// claimView builds the query projection from the store: the quads of the trusted claims' graphs
// (named), and the publication info of every claim the union holds (pubinfo).
func (d *Daemon) claimView(ctx context.Context, named, pubinfo []string, vc ViewConfig) ([]Quad, error) {
	trusted, err := d.quadsOf(ctx, named)
	if err != nil {
		return nil, err
	}
	structure, err := d.quadsOf(ctx, pubinfo)
	if err != nil {
		return nil, err
	}
	return ClaimView(trusted, structure, vc), nil
}

// quadsOf reads the quads of the named graphs, each graph by its name: a bound graph is one lookup,
// where a graph listed in the dataset is a scan per row.
func (d *Daemon) quadsOf(ctx context.Context, graphs []string) ([]Quad, error) {
	var out []Quad
	const chunk = 4000
	for i := 0; i < len(graphs); i += chunk {
		var vals strings.Builder
		for _, g := range graphs[i:min(i+chunk, len(graphs))] {
			vals.WriteString(" <" + g + ">")
		}
		body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {
			"SELECT ?g ?s ?p ?o WHERE { VALUES ?g {" + vals.String() + " } GRAPH ?g { ?s ?p ?o } }"}})
		if err != nil {
			return nil, err
		}
		res, err := ParseResults(body)
		if err != nil {
			return nil, err
		}
		for _, t := range res.Terms {
			out = append(out, Quad{G: t["g"].Value, S: t["s"], P: t["p"], O: t["o"]})
		}
	}
	return out, nil
}

// NQuads writes graphs as N-Quads, each quad in the graph it is filed under.
func NQuads(graphs map[string][]Quad) []byte {
	var b strings.Builder
	for _, g := range sortedKeys(graphs) {
		for _, q := range graphs[g] {
			b.WriteString(ntTerm(q.S) + " " + ntTerm(q.P) + " " + ntTerm(q.O) + " <" + g + "> .\n")
		}
	}
	return []byte(b.String())
}
