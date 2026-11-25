package oxigraph

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	nclaim "kton.dev/nekton/claim"
	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// ADR-022. What the index records about the chains of the registries it reads, and what a view
// makes of it. The kernel decides whether a claim's chain resolves; the index writes down what each
// registry's kernel said and never settles a chain itself:
//
//   - urn:kton:links holds, for every record a nekton registry returned (read from the envelopes,
//     whatever the projection makes of them), the record's own scope, predecessor and whether it is a seed, read from its
//     envelope. The triples are the same whichever registry writes them: they belong to the claim.
//   - <registry> urn:kton:resolves <claim> when that registry's kernel holds the claim with its chain
//     resolved, and <registry> urn:kton:defers <claim> when it holds it waiting for a seed or a
//     predecessor (the kernel offers such a claim in its feed, but a query never sees it).
//   - <graph> urn:kton:arrived:<registry> n, a number from the registry's own counter urn:kton:arrivals,
//     for every record graph and attribution graph the registry brought for the first time.
//   - <claim> urn:kton:settled:<registry> n, numbered from the same counter, for a claim the registry held
//     waiting and its kernel now holds resolved: no record came, yet a view built before has to take it in.
//   - <registry> urn:kton:generation counts the times a read found something the registry no longer holds.
//
// A view is patched from these (patch.go), not rebuilt.

const (
	linksGraph = kNS + "links"
	xsdInteger = "http://www.w3.org/2001/XMLSchema#integer"
)

func chainIRI(slot string) string { return kNS + "view:" + slot + ":chain" }

// chainRecord is one record's place in its chain as its envelope states it: the hex of the claim, of
// its scope's seed and of its predecessor ("" for none).
type chainRecord struct {
	hex, scope, prev string
	seed             bool
}

// chainRecords reads the chain links of every record recs holds, whatever projectNekton makes of them: the
// chain graph is built from the envelopes and does not depend on the projection.
func chainRecords(recs []nregistry.Record) []chainRecord {
	var out []chainRecord
	norm := func(v string) string {
		if n, ok := core.NormalizeContentHash(v); ok {
			return strings.TrimPrefix(n, "sha256:")
		}
		return ""
	}
	for _, rec := range recs {
		st, payload, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil {
			continue
		}
		c := chainRecord{hex: strings.TrimPrefix(nclaim.ClaimID(payload), "sha256:")}
		if st.IsSeed() {
			c.seed, c.scope = true, c.hex
		} else {
			p, err := st.ParsePredicate()
			if err != nil {
				continue
			}
			c.scope, c.prev = norm(p.Scope), norm(p.Prev)
		}
		out = append(out, c)
	}
	return out
}

// linkNQuads writes the chain links as N-Quads in the links graph. Content-addressed: set semantics
// make writing a claim twice, from two registries, one set of triples.
func linkNQuads(cs []chainRecord) string {
	var b strings.Builder
	for _, c := range cs {
		n := "<" + oNS + c.hex + ">"
		if c.scope != "" {
			fmt.Fprintf(&b, "%s <%sscope> <%s%s> <%s> .\n", n, nkNS, oNS, c.scope, linksGraph)
		}
		if c.prev != "" {
			fmt.Fprintf(&b, "%s <%sprev> <%s%s> <%s> .\n", n, nkNS, oNS, c.prev, linksGraph)
		}
		if c.seed {
			fmt.Fprintf(&b, "%s <%sgenesis> \"true\"^^<%sboolean> <%s> .\n", n, nkNS, xsdNS, linksGraph)
		}
	}
	return b.String()
}

// kernelSaid is what a registry's kernel says about its records, to be written next to them: the
// claims it holds with their chain resolved, and those it holds waiting.
type kernelSaid struct {
	resolves, defers []string // claim hexes, sorted
}

// write adds to u the updates that record what the kernel said: each claim it holds with its chain
// resolved is resolved and no longer deferred, each held waiting the reverse. A full read has already
// cleared both lists, so it only inserts.
func (k kernelSaid) write(u *strings.Builder, reg string, full bool) {
	var ins strings.Builder
	for _, h := range k.resolves {
		fmt.Fprintf(&ins, "<%s> <%sresolves> <%s%s> .\n", reg, kNS, oNS, h)
		if !full {
			fmt.Fprintf(u, "DELETE DATA { GRAPH <%s> { <%s> <%sdefers> <%s%s> } } ;\n", indexGraph, reg, kNS, oNS, h)
		}
	}
	for _, h := range k.defers {
		fmt.Fprintf(&ins, "<%s> <%sdefers> <%s%s> .\n", reg, kNS, oNS, h)
		if !full {
			fmt.Fprintf(u, "DELETE DATA { GRAPH <%s> { <%s> <%sresolves> <%s%s> } } ;\n", indexGraph, reg, kNS, oNS, h)
		}
	}
	if ins.Len() > 0 {
		fmt.Fprintf(u, "INSERT DATA { GRAPH <%s> { %s } } ;\n", indexGraph, ins.String())
	}
}

// askKernel asks r about each id: held (Claim) or held waiting (DeferredClaim). A claim the kernel
// holds neither way is left out, it is not in the registry.
func askKernel(r *nregistry.Registry, ids []string) kernelSaid {
	var k kernelSaid
	for _, h := range ids {
		if _, ok := r.Claim("sha256:" + h); ok {
			k.resolves = append(k.resolves, h)
		} else if _, _, ok := r.DeferredClaim("sha256:" + h); ok {
			k.defers = append(k.defers, h)
		}
	}
	return k
}

// unseen are the graphs of gs that reg has no p triple for yet in the index.
func (d *Daemon) unseen(ctx context.Context, reg, p string, gs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, g := range gs {
		out[g] = true
	}
	const chunk = 4000
	for i := 0; i < len(gs); i += chunk {
		var vals strings.Builder
		for _, g := range gs[i:min(i+chunk, len(gs))] {
			vals.WriteString(" <" + g + ">")
		}
		rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
			`SELECT ?g WHERE { GRAPH <%s> { VALUES ?g {%s } <%s> <%s%s> ?g } }`, indexGraph, vals.String(), reg, kNS, p)}})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			delete(out, r["g"])
		}
	}
	return out, nil
}

// previouslyDeferred are the claims reg's kernel held waiting when it was last read. The kernel does
// not feed a claim again when it settles (its feed entry keeps the original number), so an
// incremental read asks about these by id.
func (d *Daemon) previouslyDeferred(ctx context.Context, reg string) ([]string, error) {
	rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
		`SELECT ?c WHERE { GRAPH <%s> { <%s> <%sdefers> ?c } }`, indexGraph, reg, kNS)}})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		out = append(out, strings.TrimPrefix(r["c"], oNS))
	}
	return out, nil
}

// chainInfo is what a view knows of the chains of its registries: which claims resolved in them and
// where each stands.
type chainInfo struct {
	resolved   map[string]bool
	links      map[string]chainLink // of the resolved claims
	genesis    map[string]bool      // the seeds among them
	positions  map[string]int
	unresolved map[string]int // by scope hex: the claims held waiting that no registry settled
}

// chainOf reads which claims are resolved in regs: those some registry's kernel resolved, and, of the
// claims every registry holds waiting, those the union of the slot's nekton directories settles
// (a seed in one registry, its claims in another). The union is the kernel's own settling, opened only
// when such claims exist.
func (d *Daemon) chainOf(ctx context.Context, regs, nektonDirs []string) (*chainInfo, error) {
	vals := make([]string, len(regs))
	for i, r := range regs {
		vals[i] = "<" + r + ">"
	}
	rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?c ?p WHERE { GRAPH <%s> {
  VALUES ?r { %s } ?r ?p ?c FILTER(?p IN (<%sresolves>, <%sdefers>)) } }`, indexGraph, strings.Join(vals, " "), kNS, kNS)}})
	if err != nil {
		return nil, err
	}
	ci := &chainInfo{resolved: map[string]bool{}, genesis: map[string]bool{}, unresolved: map[string]int{}}
	waiting := map[string]bool{}
	for _, r := range rows {
		h := strings.TrimPrefix(r["c"], oNS)
		if r["p"] == kNS+"resolves" {
			ci.resolved[h] = true
		} else {
			waiting[h] = true
		}
	}
	var held []string
	for h := range waiting {
		if !ci.resolved[h] {
			held = append(held, h)
		}
	}
	sort.Strings(held)
	if len(held) > 0 {
		var dirs []string
		for _, dir := range nektonDirs {
			if binaries.Exists(dir) {
				dirs = append(dirs, dir)
			}
		}
		if len(dirs) > 0 {
			u, err := nregistry.OpenUnion(dirs...)
			if err != nil {
				return nil, fmt.Errorf("settling the claims every registry holds waiting: %w", err)
			}
			rest := held[:0]
			for _, h := range held {
				if _, ok := u.Claim("sha256:" + h); ok {
					ci.resolved[h] = true
				} else {
					rest = append(rest, h)
				}
			}
			held = rest
		}
	}

	all := make([]string, 0, len(ci.resolved)+len(held))
	for h := range ci.resolved {
		all = append(all, h)
	}
	all = append(all, held...)
	sort.Strings(all)
	links := map[string]chainLink{}
	const chunk = 4000
	for i := 0; i < len(all); i += chunk {
		var vs strings.Builder
		for _, h := range all[i:min(i+chunk, len(all))] {
			vs.WriteString(" <" + oNS + h + ">")
		}
		rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
			`SELECT ?s ?p ?o WHERE { GRAPH <%s> { VALUES ?s {%s } ?s ?p ?o } }`, linksGraph, vs.String())}})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			h := strings.TrimPrefix(r["s"], oNS)
			l := links[h]
			switch r["p"] {
			case nkNS + "scope":
				l[0] = strings.TrimPrefix(r["o"], oNS)
			case nkNS + "prev":
				l[1] = strings.TrimPrefix(r["o"], oNS)
			case nkNS + "genesis":
				ci.genesis[h] = true
			}
			links[h] = l
		}
	}
	ci.links = map[string]chainLink{}
	for h := range ci.resolved {
		ci.links[h] = links[h]
	}
	for _, h := range held {
		if s := links[h][0]; s != "" {
			ci.unresolved[s]++
		}
	}
	ci.positions = positionsOf(ci.links, func(h string) bool { return ci.resolved[h] })
	return ci, nil
}

// quads is the chain graph of a view: for every resolved claim, trusted or not, its scope, its
// predecessor, whether it is a seed and its position, and per scope the number of claims held waiting
// (ADR-022 D2). It is the index's own bookkeeping and in no query's dataset.
func (ci *chainInfo) quads() []Quad {
	var out []Quad
	hexes := make([]string, 0, len(ci.links))
	for h := range ci.links {
		hexes = append(hexes, h)
	}
	sort.Strings(hexes)
	for _, h := range hexes {
		n, l := oNS+h, ci.links[h]
		if l[0] != "" {
			out = append(out, triple(n, nkNS+"scope", iri(oNS+l[0])))
		}
		if l[1] != "" {
			out = append(out, triple(n, nkNS+"prev", iri(oNS+l[1])))
		}
		if ci.genesis[h] {
			out = append(out, triple(n, nkNS+"genesis", typed("true", xsdNS+"boolean")))
		}
		if p, ok := ci.positions[h]; ok && p >= 0 {
			out = append(out, triple(n, nkNS+"position", typed(strconv.Itoa(p), xsdInteger)))
		}
	}
	scopes := make([]string, 0, len(ci.unresolved))
	for s := range ci.unresolved {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)
	for _, s := range scopes {
		out = append(out, triple(oNS+s, kNS+"unresolved", typed(strconv.Itoa(ci.unresolved[s]), xsdInteger)))
	}
	return out
}

// ScopeHead reads a scope's chain the way `nekton head` reports it, from the chain graph of cfg's view
// under keys (ADR-022 D3). It reads into the view first whatever changed in EVERY registry of the slot,
// never only the repository's own: a claim a teammate's registry brought since the view was made must
// be in the head, or a claim said now would branch the scope.
//
// The heads are the resolved in-scope claims no resolved in-scope claim names as its predecessor, the
// seed when the scope has none; the seed is not a member of its scope. An error, or a seed the view
// does not hold resolved, is the caller's cue to read the union instead: the index never decides that
// a scope does not exist.
func ScopeHead(ctx context.Context, d *Daemon, cfg *config.Config, keys []ed25519.PublicKey, scope string) (*binaries.ScopeHead, error) {
	ds, err := d.prepare(ctx, cfg, keys)
	if err != nil {
		return nil, err
	}
	seed := strings.TrimPrefix(scope, "sha256:")
	if !isHex(seed) {
		return nil, fmt.Errorf("scope %q is not a claim id", scope)
	}
	g, s := chainIRI(ds.slot), "<"+oNS+seed+">"
	gen, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
		`SELECT ?g WHERE { GRAPH <%s> { %s <%sgenesis> ?g } }`, g, s, nkNS)}})
	if err != nil {
		return nil, err
	}
	if len(gen) == 0 {
		return nil, fmt.Errorf("the index's view holds no resolved seed %s", scope)
	}
	rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
		`SELECT ?c ?prev WHERE { GRAPH <%s> { ?c <%sscope> %s . FILTER(?c != %s) OPTIONAL { ?c <%sprev> ?prev } } }`,
		g, nkNS, s, s, nkNS)}})
	if err != nil {
		return nil, err
	}
	members, named := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		members[strings.TrimPrefix(r["c"], oNS)] = true
		if p := r["prev"]; p != "" {
			named[strings.TrimPrefix(p, oNS)] = true
		}
	}
	var heads []string
	for h := range members {
		if !named[h] {
			heads = append(heads, "sha256:"+h)
		}
	}
	sort.Strings(heads)
	if len(members) == 0 {
		heads = []string{"sha256:" + seed}
	}
	un, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
		`SELECT ?n WHERE { GRAPH <%s> { %s <%sunresolved> ?n } }`, g, s, kNS)}})
	if err != nil {
		return nil, err
	}
	unresolved := 0
	if len(un) > 0 {
		if unresolved, err = strconv.Atoi(un[0]["n"]); err != nil {
			return nil, fmt.Errorf("the unresolved count of %s: %w", scope, err)
		}
	}
	return &binaries.ScopeHead{Scope: scope, Heads: heads, ChainLength: len(members),
		Branched: len(heads) > 1, Unresolved: unresolved}, nil
}

func isHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
