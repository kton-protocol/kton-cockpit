package oxigraph

import (
	"strings"
	"testing"

	ntemplate "kton.dev/nekton/template"
)

// claimQuads is one claim as the kernel projects it: its assertion, its publication info and, when
// signed by a trusted key, its attribution graph.
func claimQuads(h, subject, pred string, fields map[string]string, created, scope, prev, kid string) (all, pubinfo []Quad) {
	np := npNS + h + "."
	a, i := np+"#assertion", np+"#pubinfo"
	helper := np + "#o"
	all = append(all, Quad{G: a, S: iri(subject), P: iri(pred), O: iri(helper)})
	for k, v := range fields {
		all = append(all, Quad{G: a, S: iri(helper), P: iri(labNS + k), O: lit(v)})
	}
	pubinfo = append(pubinfo, Quad{G: i, S: iri(np), P: iri(dctTime), O: typed(created, xsdNS+"dateTime")})
	if scope != "" {
		pubinfo = append(pubinfo, Quad{G: i, S: iri(np), P: iri(nkNS + "scope"), O: iri(oNS + scope)})
	}
	if prev != "" {
		pubinfo = append(pubinfo, Quad{G: i, S: iri(np), P: iri(nkNS + "prev"), O: iri(oNS + prev)})
	}
	all = append(all, pubinfo...)
	if kid != "" {
		all = append(all, Quad{G: attrNS + h + "/" + kid, S: iri(np), P: iri("http://purl.org/dc/terms/creator"), O: iri(agentNS + kid)})
	}
	return all, pubinfo
}

// Positions count every claim of a chain, trusted or not; only trusted claims are shown, and only
// they supersede one another: the latest trusted claim per scope, subject and predicate is current.
func TestClaimView_KeepsPositionAndCurrent(t *testing.T) {
	const seed, c1, u, c2, other = "aa", "b1", "cc", "d2", "ee"
	const step = oNS + "f00d"
	desc := "http://purl.org/dc/terms/description"
	var trusted, structure []Quad
	add := func(all, pub []Quad) {
		structure = append(structure, pub...)
		trusted = append(trusted, all...)
	}
	// A seed's assertion names its scope, with no helper node.
	sa, sp := claimQuads(seed, "urn:nekton:scope:study", rdfType, nil, "2026-01-01T00:00:00Z", seed, "", "k1")
	sa = sa[1:]
	sa = append(sa,
		Quad{G: npNS + seed + ".#assertion", S: iri("urn:nekton:scope:study"), P: iri(rdfType), O: iri(nkNS + "Scope")},
		Quad{G: npNS + seed + ".#assertion", S: iri("urn:nekton:scope:study"), P: iri(rdfsLbl), O: lit("study")},
		Quad{G: npNS + seed + ".#pubinfo", S: iri(npNS + seed + "."), P: iri(nkNS + "genesis"), O: typed("true", xsdNS+"boolean")})
	add(sa, sp)
	add(claimQuads(c1, step, desc, map[string]string{"value": "first"}, "2026-01-02T00:00:00Z", seed, seed, "k1"))
	_, pub := claimQuads(u, step, desc, map[string]string{"value": "by a stranger"}, "2026-01-03T00:00:00Z", seed, c1, "")
	structure = append(structure, pub...) // in the chain, signed by nobody trusted
	add(claimQuads(c2, step, desc, map[string]string{"value": "second"}, "2026-01-04T00:00:00Z", seed, u, "k1"))
	add(claimQuads(other, step, desc, map[string]string{"value": "unscoped"}, "2026-01-05T00:00:00Z", "", "", "k1"))

	view := ClaimView(trusted, structure, ViewConfig{Signers: map[string]Signer{"k1": {Name: "alice", Tier: "self"}}})
	if got := values(view, oNS+u, rdfType); len(got) != 0 {
		t.Errorf("an untrusted claim is shown: %v", got)
	}
	for h, want := range map[string]string{seed: "0", c1: "1", c2: "3"} {
		if got := values(view, oNS+h, nkNS+"position"); len(got) != 1 || got[0] != want {
			t.Errorf("position of %s: got %v, want %s (the untrusted claim counts)", h, got, want)
		}
	}
	for h, want := range map[string]string{c1: "false", c2: "true", other: "true"} {
		if got := values(view, oNS+h, nkNS+"current"); len(got) != 1 || got[0] != want {
			t.Errorf("current of %s: got %v, want %s", h, got, want)
		}
	}
	if got := values(view, oNS+c2, nkNS+"about"); len(got) != 1 || got[0] != step {
		t.Errorf("about: %v", got)
	}
	if got := values(view, oNS+c2, labNS+"value"); len(got) != 1 || got[0] != "second" {
		t.Errorf("a field on the claim node: %v", got)
	}
	if got := values(view, agentNS+"k1", rdfsLbl); len(got) != 1 || got[0] != "alice" {
		t.Errorf("the signer's name: %v", got)
	}
	if got := values(view, oNS+seed, rdfsLbl); len(got) != 1 || got[0] != "study" {
		t.Errorf("a seed carries its scope's name: %v", got)
	}
}

// A template's aliases give a field its term, and its field types give a value its type.
func TestClaimView_AppliesTheTemplatesTermsAndTypes(t *testing.T) {
	set, err := ntemplate.New(map[string][]byte{
		"reviewed": []byte(`{"name":"reviewed","target":"either","predicate":"https://example.org/q/reviewed",
		  "fields":{"on":{"type":"date"},"outcome":{"type":"string"},"basis":{"type":"ref"}}}`),
	}, []byte(`{"prefixes":{"q":"https://example.org/q/"},"terms":{"outcome":"q:outcome"}}`))
	if err != nil {
		t.Fatal(err)
	}
	all, pub := claimQuads("ab", oNS+"f00d", "https://example.org/q/reviewed",
		map[string]string{"on": "2026-03-04", "outcome": "accepted", "basis": "sha256:beef"}, "2026-03-04T10:00:00Z", "", "", "k1")
	view := ClaimView(all, pub, ViewConfig{Templates: set})
	n := oNS + "ab"
	if got := values(view, n, "https://example.org/q/outcome"); len(got) != 1 || got[0] != "accepted" {
		t.Errorf("the alias's term: %v", got)
	}
	if got := values(view, n, nkNS+"template"); len(got) != 1 || got[0] != "reviewed" {
		t.Errorf("the template's name: %v", got)
	}
	for _, q := range view {
		switch q.P.Value {
		case labNS + "on":
			if q.O.Datatype != xsdNS+"date" {
				t.Errorf("a date field: %+v", q.O)
			}
		case labNS + "basis":
			if q.O.Type != "uri" || q.O.Value != oNS+"beef" {
				t.Errorf("a ref field: %+v", q.O)
			}
		}
	}
}

// A ref field the kernel projects as a node of its own (#o.<field> with lab:hash) points at what it
// names.
func TestClaimView_ARefFieldPointsAtWhatItNames(t *testing.T) {
	all, pub := claimQuads("ac", oNS+"f00d", "https://example.org/q/derived", nil, "2026-03-04T10:00:00Z", "", "", "k1")
	inner := npNS + "ac.#o.pa"
	all = append(all,
		Quad{G: npNS + "ac.#assertion", S: iri(npNS + "ac.#o"), P: iri(labNS + "parent-step"), O: iri(inner)},
		Quad{G: npNS + "ac.#assertion", S: iri(inner), P: iri(labNS + "hash"), O: iri(oNS + "beef")})
	view := ClaimView(all, pub, ViewConfig{})
	if got := values(view, oNS+"ac", labNS+"parent-step"); len(got) != 1 || got[0] != oNS+"beef" {
		t.Errorf("want the step it names: %v", got)
	}
	for _, q := range view {
		if strings.HasPrefix(q.S.Value, npNS) || strings.HasPrefix(q.O.Value, npNS) {
			t.Errorf("a node inside the nanopublication leaked into the view: %+v", q)
		}
	}
}

// The kernel's N-Quads read back as they were written, escapes included.
func TestParseNQuads_ReadsWhatNTriplesWrites(t *testing.T) {
	in := []Quad{
		{S: iri("https://a/s"), P: iri("https://a/p"), O: lit("line one\nwith \"quotes\" and \\ and \t tab")},
		{S: iri("https://a/s"), P: iri("https://a/p"), O: typed("3", xsdNS+"integer")},
		{S: Term{Type: "bnode", Value: "b0"}, P: iri("https://a/p"), O: Term{Type: "literal", Value: "hallo", Lang: "de"}},
	}
	out, err := ParseNQuads(NTriples(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d quads", len(out))
	}
	for i := range in {
		if out[i].S != in[i].S || out[i].P != in[i].P || out[i].O != in[i].O {
			t.Errorf("quad %d: got %+v, want %+v", i, out[i], in[i])
		}
	}
	if _, err := ParseNQuads([]byte(`<a> <b> "unclosed .`)); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("a broken line: want an error naming it, got %v", err)
	}
}
