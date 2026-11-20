package oxigraph

import (
	"github.com/kton-protocol/kton-cockpit/internal/packages"
	"math/rand"
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

// Positions count every claim of a chain, trusted or not; only trusted claims are shown. Nothing is
// inferred from the order (view vocabulary 1): a later claim does not make an earlier one anything.
func TestClaimView_KeepsPositionAndInfersNothing(t *testing.T) {
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
	for _, h := range []string{c1, c2, other} {
		for _, p := range []string{"current", "supersededBy", "replacedBy"} {
			if got := values(view, oNS+h, nkNS+p); len(got) != 0 {
				t.Errorf("nk:%s inferred on %s: %v", p, h, got)
			}
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
		  "fields":{"on":{"type":"date"},"outcome":{"type":"string"},"basis":{"type":"ref"},"revision":{"type":"integer"},
		    "state":{"type":"enum","values":["q:Effective","q:Draft"]},"owner":{"type":"enum","values":["https://example.org/r/qa"]},
		    "verdict":{"type":"enum","values":["approve","reject"]},
		    "document":{"type":"iri"},"spec":{"type":"iri"},"note":{"type":"iri"}}}`),
	}, []byte(`{"prefixes":{"q":"https://example.org/q/"},"terms":{"outcome":"q:outcome"}}`))
	if err != nil {
		t.Fatal(err)
	}
	all, pub := claimQuads("ab", oNS+"f00d", "https://example.org/q/reviewed",
		map[string]string{"on": "2026-03-04", "outcome": "accepted", "basis": "sha256:beef", "revision": "12",
			"state": "q:Effective", "owner": "https://example.org/r/qa", "verdict": "approve",
			"document": "urn:qms:doc:SOP-12", "spec": "q:spec-7", "note": "not an iri"}, "2026-03-04T10:00:00Z", "", "", "k1")
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
		case labNS + "revision":
			if q.O.Datatype != xsdNS+"integer" || q.O.Value != "12" {
				t.Errorf("an integer field: %+v", q.O)
			}
		case labNS + "state":
			if q.O.Type != "uri" || q.O.Value != "https://example.org/q/Effective" {
				t.Errorf("an enum value written as a CURIE: %+v", q.O)
			}
		case labNS + "owner":
			if q.O.Type != "uri" || q.O.Value != "https://example.org/r/qa" {
				t.Errorf("an enum value written as an IRI: %+v", q.O)
			}
		case labNS + "verdict":
			if q.O.Type != "literal" || q.O.Value != "approve" {
				t.Errorf("a bare enum value stays a literal: %+v", q.O)
			}
		case labNS + "document":
			if q.O.Type != "uri" || q.O.Value != "urn:qms:doc:SOP-12" {
				t.Errorf("an iri field with a urn: %+v", q.O)
			}
		case labNS + "spec":
			if q.O.Type != "uri" || q.O.Value != "https://example.org/q/spec-7" {
				t.Errorf("an iri field written as a CURIE: %+v", q.O)
			}
		case labNS + "note":
			if q.O.Type != "literal" {
				t.Errorf("an iri field that is not one stays as signed: %+v", q.O)
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

// replacing is one quad of the replacing claim's field node, as the kernel projects the reserved
// field: helper nk:replaces <o/hex> (kton #257, #258).
func replacing(h, old string) Quad {
	return Quad{G: npNS + h + ".#assertion", S: iri(npNS + h + ".#o"), P: iri(nkNS + "replaces"), O: iri(oNS + old)}
}

// claimQuadsBy is claimQuads with an attribution graph for each further co-signer.
func claimQuadsBy(h, subject, pred, created string, kids ...string) (all, pubinfo []Quad) {
	all, pubinfo = claimQuads(h, subject, pred, nil, created, "", "", kids[0])
	for _, kid := range kids[1:] {
		all = append(all, Quad{G: attrNS + h + "/" + kid, S: iri(npNS + h + "."), P: iri("http://purl.org/dc/terms/creator"), O: iri(agentNS + kid)})
	}
	return all, pubinfo
}

// ADR-013 A5: every name in replaces is projected as nk:replaces, whoever signed either claim; a
// replacement retires a claim with nk:replacedBy only when every signer of the replaced claim signed
// it too, it has the same template, and the two share a subject. kton-review M1: (a) a different
// key's replaces leaves an approval standing; M3: (b) a name in upper case still marks; C1: one of
// two co-signers cannot retire a co-signed claim, both together can.
func TestClaimView_ReplacedByOnlyForTheSameSignerTemplateAndSubject(t *testing.T) {
	const model, other = "https://ex.org/model", "https://ex.org/other"
	approval, status := "https://ex.org/approval", "https://ex.org/status"
	var trusted, structure []Quad
	add := func(h, subject, pred, created, old string, kids ...string) {
		a, p := claimQuadsBy(h, subject, pred, created, kids...)
		if old != "" {
			a = append(a, replacing(h, old))
		}
		trusted, structure = append(trusted, a...), append(structure, p...)
	}
	add("a1", model, approval, "2026-01-01T00:00:00Z", "", "k1")
	add("a2", model, approval, "2026-01-02T00:00:00Z", "a1", "k1")
	add("b1", model, approval, "2026-01-03T00:00:00Z", "a2", "k2") // (a) another key names a2: a2 stands
	add("c1", other, approval, "2026-01-04T00:00:00Z", "", "k1")
	add("c2", other, approval, "2026-01-05T00:00:00Z", "C1", "k1") // (b) upper case still names c1
	add("d1", model, status, "2026-01-06T00:00:00Z", "", "k1")
	add("d2", model, approval, "2026-01-07T00:00:00Z", "d1", "k1") // another template: retires nothing
	add("e1", other, approval, "2026-01-08T00:00:00Z", "", "k1")
	add("e2", model, approval, "2026-01-09T00:00:00Z", "e1", "k1") // another subject: retires nothing
	add("f1", model, approval, "2026-01-10T00:00:00Z", "", "k1", "k3")
	add("f2", model, approval, "2026-01-11T00:00:00Z", "f1", "k3") // C1: one co-signer alone: f1 stands
	add("g1", other, approval, "2026-01-12T00:00:00Z", "", "k1", "k3")
	add("g2", other, approval, "2026-01-13T00:00:00Z", "g1", "k3", "k1") // both co-signers: g1 retired

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		rng.Shuffle(len(trusted), func(a, b int) { trusted[a], trusted[b] = trusted[b], trusted[a] })
		rng.Shuffle(len(structure), func(a, b int) { structure[a], structure[b] = structure[b], structure[a] })
		view := ClaimView(trusted, structure, ViewConfig{})
		for h, want := range map[string]string{"a1": "a2", "a2": "", "c1": "c2", "d1": "", "e1": "", "f1": "", "g1": "g2"} {
			got := values(view, oNS+h, nkNS+"replacedBy")
			if (want == "" && len(got) != 0) || (want != "" && (len(got) != 1 || got[0] != oNS+want)) {
				t.Fatalf("build %d: %s replacedBy %v, want %q", i, h, got, want)
			}
		}
		for h, want := range map[string]string{"a2": "a1", "b1": "a2", "c2": "c1", "d2": "d1", "e2": "e1", "f2": "f1", "g2": "g1"} {
			if got := values(view, oNS+h, nkNS+"replaces"); len(got) != 1 || got[0] != oNS+want {
				t.Fatalf("build %d: %s replaces %v, want %s (every name is projected, in lower case)", i, h, got, want)
			}
		}
	}
}

// A claim about several things is about each of them (kton #254), and a seed names its parent
// scope's seed (kton #241).
func TestClaimView_EveryAboutAndASeedsParent(t *testing.T) {
	const parent, seed = "a0", "b0"
	np := npNS + "c1."
	all, pub := claimQuads("c1", "https://ex.org/x", "https://ex.org/status", nil, "2026-01-01T00:00:00Z", "", "", "k1")
	all = append(all, Quad{G: np + "#assertion", S: iri("https://ex.org/y"), P: iri("https://ex.org/status"), O: iri(np + "#o")})

	sa, sp := claimQuads(seed, "urn:nekton:scope:study", rdfType, nil, "2026-01-01T00:00:00Z", seed, "", "k1")
	sa = append(sa[1:],
		Quad{G: npNS + seed + ".#assertion", S: iri("urn:nekton:scope:study"), P: iri(rdfsLbl), O: lit("study")},
		Quad{G: npNS + seed + ".#assertion", S: iri("urn:nekton:scope:study"), P: iri(nkNS + "parent"), O: iri(oNS + "A0")},
		Quad{G: npNS + seed + ".#pubinfo", S: iri(npNS + seed + "."), P: iri(nkNS + "genesis"), O: typed("true", xsdNS+"boolean")})

	view := ClaimView(append(all, sa...), append(pub, sp...), ViewConfig{})
	if got := values(view, oNS+"c1", nkNS+"about"); len(got) != 2 {
		t.Errorf("a claim about two things: nk:about %v", got)
	}
	if got := values(view, oNS+seed, nkNS+"parent"); len(got) != 1 || got[0] != oNS+parent {
		t.Errorf("the seed's parent: %v, want %s", got, oNS+parent)
	}
	if got := values(view, oNS+"c1", nkNS+"parent"); len(got) != 0 {
		t.Errorf("only a seed has a parent: %v", got)
	}
}

// A field aliased to a term the view writes itself stays under its lab name: it cannot add a second
// nk:signer beside the one verification gives (review A1).
func TestClaimView_AnAliasCannotNameATermTheViewWrites(t *testing.T) {
	set, err := ntemplate.New(nil, []byte(`{"terms":{"reviewer":"https://kton.dev/v/signer"}}`))
	if err != nil {
		t.Fatal(err)
	}
	all, pub := claimQuads("ad", oNS+"f00d", "https://example.org/q/reviewed",
		map[string]string{"reviewer": agentNS + "someone"}, "2026-03-04T10:00:00Z", "", "", "k1")
	view := ClaimView(all, pub, ViewConfig{Templates: set})
	if got := values(view, oNS+"ad", nkNS+"signer"); len(got) != 1 || got[0] != agentNS+"k1" {
		t.Errorf("want only the verifying key as signer: %v", got)
	}
	if got := values(view, oNS+"ad", labNS+"reviewer"); len(got) != 1 {
		t.Errorf("want the field under its lab name: %v", got)
	}
}

// A nested object's fields (a list of objects, such as a qualification's role and level) go through
// the aliases as a top-level field's do; a field without an alias stays lab:<field> (the C3 training
// area's gap 3).
func TestClaimView_ANestedObjectsFieldsGoThroughTheAliases(t *testing.T) {
	set, err := ntemplate.New(map[string][]byte{
		"qualified": []byte(`{"name":"qualified","target":"either","predicate":"https://ex.org/qualified","fields":{"quals":{"type":"string"}}}`),
	}, []byte(`{"terms":{"role":"https://example.org/q/role"}}`))
	if err != nil {
		t.Fatal(err)
	}
	np := npNS + "c1."
	helper := np + "#o"
	nested := helper + ".quals"
	all, pub := claimQuads("c1", "https://ex.org/person", "https://ex.org/qualified", nil, "2026-11-19T10:00:00Z", "", "", "k1")
	all = append(all,
		Quad{G: np + "#assertion", S: iri(helper), P: iri(labNS + "quals"), O: iri(nested)},
		Quad{G: np + "#assertion", S: iri(nested), P: iri(labNS + "role"), O: lit("auditor")},
		Quad{G: np + "#assertion", S: iri(nested), P: iri(labNS + "level"), O: lit("2")},
	)
	view := ClaimView(all, pub, ViewConfig{Templates: set})
	if got := values(view, nested, "https://example.org/q/role"); len(got) != 1 || got[0] != "auditor" {
		t.Errorf("the nested role is not under its alias: %v", got)
	}
	if got := values(view, nested, labNS+"level"); len(got) != 1 || got[0] != "2" {
		t.Errorf("an unaliased nested field should stay lab:level: %v", got)
	}
	if got := values(view, nested, labNS+"role"); len(got) != 0 {
		t.Errorf("the nested role is still under lab:role too: %v", got)
	}
}

// A package template's fields resolve through its own package's aliases, never another package's; the
// repository's aliases resolve what the package leaves as it is, and a claim of a repository template;
// nested objects go through them at any depth (SPEC §9.11, kton-review A1 on #164).
func TestClaimView_APackagesFieldsResolveThroughItsOwnAliasesOnly(t *testing.T) {
	set, err := ntemplate.New(map[string][]byte{
		"package:one": []byte(`{"name":"one","predicate":"https://ex.org/one","fields":{"outcome":{"type":"enum","values":["q:ok"]},"quals":{"type":"string"}}}`),
		"package:two": []byte(`{"name":"two","predicate":"https://ex.org/two","fields":{"outcome":{"type":"string"}}}`),
		"repo":        []byte(`{"name":"repo","predicate":"https://ex.org/repo","fields":{"outcome":{"type":"string"},"note":{"type":"string"}}}`),
	}, []byte(`{"terms":{"outcome":"https://ex.org/repo/outcome","note":"https://ex.org/repo/note"}}`))
	if err != nil {
		t.Fatal(err)
	}
	vc := ViewConfig{Templates: set, Aliases: map[string]packages.Aliases{
		"one": {Prefixes: map[string]string{"q": "https://ex.org/one/"}, Terms: map[string]string{"outcome": "q:outcome", "role": "q:role", "level": "q:level"}},
		"two": {Terms: map[string]string{"outcome": "https://ex.org/two/outcome"}},
	}}
	claim := func(id, pred string, fields ...Quad) ([]Quad, []Quad) {
		all, pub := claimQuads(id, "https://ex.org/person", pred, nil, "2026-11-19T10:00:00Z", "", "", "k1")
		for _, f := range fields {
			f.G = npNS + id + ".#assertion"
			all = append(all, f)
		}
		return all, pub
	}
	h := func(id string) string { return npNS + id + ".#o" }
	a1, p1 := claim("c1", "https://ex.org/one",
		Quad{S: iri(h("c1")), P: iri(labNS + "outcome"), O: lit("q:ok")},
		Quad{S: iri(h("c1")), P: iri(labNS + "quals"), O: iri(h("c1") + ".quals")},
		Quad{S: iri(h("c1") + ".quals"), P: iri(labNS + "role"), O: iri(h("c1") + ".quals.role")},
		Quad{S: iri(h("c1") + ".quals.role"), P: iri(labNS + "level"), O: lit("2")},
		Quad{S: iri(h("c1")), P: iri(labNS + "note"), O: lit("n")})
	a2, p2 := claim("c2", "https://ex.org/two", Quad{S: iri(h("c2")), P: iri(labNS + "outcome"), O: lit("fine")})
	a3, p3 := claim("c3", "https://ex.org/repo", Quad{S: iri(h("c3")), P: iri(labNS + "outcome"), O: lit("good")})
	view := ClaimView(append(append(a1, a2...), a3...), append(append(p1, p2...), p3...), vc)

	if got := values(view, oNS+"c1", "https://ex.org/one/outcome"); len(got) != 1 || got[0] != "https://ex.org/one/ok" {
		t.Errorf("package one's outcome is not under its own term with its enum value resolved by its own prefix: %v", got)
	}
	if got := values(view, oNS+"c2", "https://ex.org/two/outcome"); len(got) != 1 || got[0] != "fine" {
		t.Errorf("package two's outcome is not under its own term: %v", got)
	}
	if got := values(view, oNS+"c3", "https://ex.org/repo/outcome"); len(got) != 1 || got[0] != "good" {
		t.Errorf("the repository template's outcome is not under the repository's term: %v", got)
	}
	for _, c := range []string{"c2", "c3"} {
		if got := values(view, oNS+c, "https://ex.org/one/outcome"); len(got) != 0 {
			t.Errorf("package one's alias resolved %s's field: %v", c, got)
		}
	}
	if got := values(view, oNS+"c1", "https://ex.org/repo/note"); len(got) != 1 {
		t.Errorf("a field the package leaves as it is does not fall back to the repository's alias: %v", got)
	}
	if got := values(view, h("c1")+".quals", "https://ex.org/one/role"); len(got) != 1 {
		t.Errorf("the nested role is not under the package's term: %v", got)
	}
	if got := values(view, h("c1")+".quals.role", "https://ex.org/one/level"); len(got) != 1 || got[0] != "2" {
		t.Errorf("a field two levels down is not under the package's term: %v", got)
	}
	other := vc
	other.Aliases = map[string]packages.Aliases{"one": vc.Aliases["two"], "two": vc.Aliases["one"]}
	if vc.Fingerprint() == other.Fingerprint() {
		t.Error("the fingerprint does not change when a package's aliases do")
	}
}

// A claim whose template this repository does not know (one a federation source signed) keeps its
// fields as lab:<field>: the repository's alias file renames the fields of the templates it resolves,
// never another source's (userinterface, 20.11.: one source's 'status' term renamed another's field;
// SPEC §9.11).
func TestClaimView_TheAliasFileRenamesOnlyItsOwnTemplatesFields(t *testing.T) {
	set, err := ntemplate.New(map[string][]byte{
		"reviewed": []byte(`{"name":"reviewed","target":"either","predicate":"https://example.org/q/reviewed",
		  "fields":{"status":{"type":"string"}}}`),
	}, []byte(`{"prefixes":{"q":"https://example.org/q/"},"terms":{"status":"q:status"}}`))
	if err != nil {
		t.Fatal(err)
	}
	own, ownPub := claimQuads("aa", oNS+"f00d", "https://example.org/q/reviewed",
		map[string]string{"status": "open"}, "2026-03-04T10:00:00Z", "", "", "k1")
	foreign, foreignPub := claimQuads("bb", oNS+"f00d", "https://example.org/elsewhere/covered",
		map[string]string{"status": "Covered"}, "2026-03-04T10:00:00Z", "", "", "k1")
	view := ClaimView(append(own, foreign...), append(ownPub, foreignPub...), ViewConfig{Templates: set})
	if got := values(view, oNS+"aa", "https://example.org/q/status"); len(got) != 1 || got[0] != "open" {
		t.Errorf("the repository's own template: want q:status, got %v", got)
	}
	if got := values(view, oNS+"bb", "https://example.org/q/status"); len(got) != 0 {
		t.Errorf("a claim of a template this repository does not know must not take its alias: %v", got)
	}
	if got := values(view, oNS+"bb", labNS+"status"); len(got) != 1 || got[0] != "Covered" {
		t.Errorf("a claim of a template this repository does not know keeps lab:status: %v", got)
	}
}

// A claim of a template whose claims are rows says nk:row true; one of a template of versions does
// not; and the flag changes the view's fingerprint, so a view built before it is built again (ADR-018,
// section 5).
func TestClaimView_ARowTemplatesClaimsSayNkRow(t *testing.T) {
	set, err := ntemplate.New(map[string][]byte{
		"covered": []byte(`{"name":"covered","target":"either","predicate":"https://example.org/q/covered","claims":"rows","fields":{"element":{"type":"string"}}}`),
		"decided": []byte(`{"name":"decided","target":"either","predicate":"https://example.org/q/decided","fields":{"outcome":{"type":"string"}}}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	row, rowPub := claimQuads("aa", oNS+"f00d", "https://example.org/q/covered", map[string]string{"element": "e1"}, "2026-03-04T10:00:00Z", "", "", "k1")
	version, versionPub := claimQuads("bb", oNS+"f00d", "https://example.org/q/decided", map[string]string{"outcome": "ok"}, "2026-03-04T10:00:00Z", "", "", "k1")
	vc := ViewConfig{Templates: set, Rows: map[string]bool{"covered": true}}
	view := ClaimView(append(row, version...), append(rowPub, versionPub...), vc)
	if got := values(view, oNS+"aa", nkNS+"row"); len(got) != 1 || got[0] != "true" {
		t.Errorf("a row template's claim: want nk:row true, got %v", got)
	}
	if got := values(view, oNS+"bb", nkNS+"row"); len(got) != 0 {
		t.Errorf("a versions template's claim says no nk:row: %v", got)
	}
	if vc.Fingerprint() == (ViewConfig{Templates: set}).Fingerprint() {
		t.Error("the rows flag must change the view's fingerprint")
	}
}
