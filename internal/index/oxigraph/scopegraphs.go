package oxigraph

import (
	"sort"
	"strconv"
)

// Unscoped marks, in the claim projection, the claims that no signed scope holds (ADR-018, section 1).
const Unscoped = "-"

// scopeGraphIRI names the graph of one scope's claims in a view; unscopedIRI the graph of the claims
// with no scope (ADR-018).
func scopeGraphIRI(slot, hex string) string { return kNS + "view:" + slot + ":scope:" + hex }
func unscopedIRI(slot string) string        { return kNS + "view:" + slot + ":unscoped" }

// scopeGraphs splits a claim projection by the graph each quad was tagged with: one graph per scope,
// one for the unscoped claims. meta describes each graph for the default graph (ADR-018, section 3):
// a nk:ClaimGraph, its seed, the seed's name and parent, and how many claims it holds besides the seed. Quads tagged
// with no graph (signer labels) are in none of them. sources names, by a seed's hex, the inputs that
// hold it: each is the graph's nk:source.
func scopeGraphs(slot string, view []Quad, sources map[string][]string) (graphs map[string][]Quad, meta []Quad) {
	graphs = map[string][]Quad{}
	count := map[string]int{}
	label, parent := map[string]Term{}, map[string]Term{}
	for _, q := range view {
		if q.G == "" {
			continue
		}
		g := scopeGraphIRI(slot, q.G)
		if q.G == Unscoped {
			g = unscopedIRI(slot)
		}
		graphs[g] = append(graphs[g], Quad{S: q.S, P: q.P, O: q.O})
		switch {
		case q.P.Value == rdfType && q.O.Value == nkNS+"Claim":
			count[q.G]++
		case q.P.Value == rdfsLbl && q.S.Value == oNS+q.G:
			label[q.G] = q.O
		case q.P.Value == nkNS+"parent" && q.S.Value == oNS+q.G:
			parent[q.G] = q.O
		case q.P.Value == nkNS+"genesis" && q.S.Value == oNS+q.G:
			count[q.G]-- // the scope's seed is in its graph, but is not one of its claims
		}
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == Unscoped {
			g := unscopedIRI(slot)
			meta = append(meta, triple(g, rdfType, iri(nkNS+"ClaimGraph")),
				triple(g, nkNS+"unscoped", typed("true", xsdNS+"boolean")),
				triple(g, nkNS+"claims", typed(strconv.Itoa(count[k]), xsdNS+"integer")))
			continue
		}
		g := scopeGraphIRI(slot, k)
		meta = append(meta, triple(g, rdfType, iri(nkNS+"ClaimGraph")),
			triple(g, nkNS+"scope", iri(oNS+k)),
			triple(g, nkNS+"claims", typed(strconv.Itoa(count[k]), xsdNS+"integer")))
		if l, ok := label[k]; ok {
			meta = append(meta, triple(g, rdfsLbl, l))
		}
		if p, ok := parent[k]; ok {
			meta = append(meta, triple(g, nkNS+"parent", p))
		}
		for _, src := range sources[k] {
			meta = append(meta, triple(g, nkNS+"source", lit(src)))
		}
	}
	return graphs, meta
}
