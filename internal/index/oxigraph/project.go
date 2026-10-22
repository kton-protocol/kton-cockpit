//go:build unreleased

package oxigraph

import (
	"crypto/ed25519"
	"regexp"
	"strings"

	nclaim "kton.dev/nekton/claim"
	"kton.dev/nekton/nanopub"
	nregistry "kton.dev/nekton/registry"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/core"
	"kton.dev/plankton/rdf"
	pregistry "kton.dev/plankton/registry"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// What a query reads, and how a record becomes RDF, is decided here once, for the daemon and for
// a container started for one query alike, so the two cannot answer differently.
//
// Each record is projected by the kernel (kton#232) in two parts: its own quads, which take no
// keys and are the same bytes on every machine, and one attribution graph per key that verifies
// it. A query's dataset is
//   - the default graph: every plankton record of the union, and the attribution of those records
//     to the keys this configuration trusts;
//   - the named graphs: the four nanopublication graphs of every claim a trusted key signed, and
//     that attribution.
// A record nobody trusted is in the default graph unattributed; a claim nobody trusted is not in
// the dataset at all, as before the index stored records once.

// projected is one record as the kernel projects it.
type projected struct {
	layer string            // "plankton" or "nekton"
	id    string            // the record's id, sha256:<hex>
	graph string            // its own graph: a foton's IRI, or a claim's nanopublication base
	own   []byte            // its own quads, canonical N-Quads
	attr  map[string][]byte // keyid -> attribution, canonical N-Quads
}

// graphs are the named graphs a record's own quads are in.
func (p projected) graphs() []string {
	if p.layer == "plankton" {
		return []string{p.graph}
	}
	return []string{p.graph + "#Head", p.graph + "#assertion", p.graph + "#provenance", p.graph + "#pubinfo"}
}

// attributionGraph is the graph the kernel put the record's attribution to kid in.
func (p projected) attributionGraph(kid string) string {
	if p.layer == "plankton" {
		return rdf.AttributionGraph(p.id, kid)
	}
	return nanopub.AttributionGraph(p.id, kid)
}

func projectPlankton(reg *pregistry.Registry, recs []pregistry.Record, keys []ed25519.PublicKey) ([]projected, error) {
	var out []projected
	for _, rec := range recs {
		own, err := rdf.Record(reg, rec.FotonID)
		if err != nil {
			return nil, err
		}
		attr, err := rdf.Attribution(reg, rec.FotonID, keys)
		if err != nil {
			return nil, err
		}
		out = append(out, projected{layer: "plankton", id: rec.FotonID, graph: rdf.RecordGraph(rec.FotonID), own: own, attr: attr})
	}
	return out, nil
}

// projectNekton skips a claim the kernel will not project (its predicate or subject would not make
// a valid IRI), as the projection before it did: such a claim answers no query.
func projectNekton(recs []nregistry.Record, keys []ed25519.PublicKey) []projected {
	ctx := nanopub.NewRecordContext(ntemplate.Set{})
	var out []projected
	for _, rec := range recs {
		own, err := nanopub.Record(ctx, rec.Envelope)
		if err != nil {
			continue
		}
		attr, err := nanopub.Attribution(ctx, rec.Envelope, keys)
		if err != nil {
			continue
		}
		_, payload, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil {
			continue
		}
		id := nclaim.ClaimID(payload)
		out = append(out, projected{layer: "nekton", id: id, graph: nanopub.RecordBase(id), own: own, attr: attr})
	}
	return out
}

// Dataset is what a container started for one query loads: the default graph as N-Triples and the
// named graphs as N-Quads, read from the union under the trusted keys.
func Dataset(u *binaries.Union, keys []ed25519.PublicKey) (defaultNT, namedNQ []byte, err error) {
	ps, err := projectPlankton(u.Plankton, u.Plankton.Records(0), keys)
	if err != nil {
		return nil, nil, err
	}
	var def, named strings.Builder
	for _, p := range ps {
		def.Write(intoDefault(p.own))
		for _, a := range p.attr {
			def.Write(intoDefault(a))
		}
	}
	for _, p := range projectNekton(u.Nekton.Records(0), keys) {
		if len(p.attr) == 0 {
			continue
		}
		named.Write(p.own)
		for _, a := range p.attr {
			named.Write(a)
		}
	}
	return []byte(def.String()), []byte(named.String()), nil
}

// trustedIDs are the key ids of keys.
func trustedIDs(keys []ed25519.PublicKey) map[string]bool {
	ids := map[string]bool{}
	for _, k := range keys {
		ids[core.KeyIDHex(k)] = true
	}
	return ids
}

// The kernel's N-Quads are canonical: one quad per line, every quad in a named graph, so the graph
// is the last IRI before the closing dot.
var graphTerm = regexp.MustCompile(`(?m)^(.*) <[^<>]*> \.$`)

// intoDefault moves quads into the default graph: N-Triples of the same statements.
func intoDefault(nq []byte) []byte { return graphTerm.ReplaceAll(nq, []byte("$1 .")) }
