package oxigraph

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"kton.dev/nekton/nanopub"
	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/flock"
)

// ADR-022 D1. A view is patched by what arrived since it was built: the graphs each registry brought
// past the number the view was built at, the claims those records touch, and nothing else. The kernel
// still decides which chains resolve (chainview.go); ClaimView's own projection (projectClaimsSpans) is
// run on the delta and its neighbours, and a property test holds the patched view equal, as sets of
// triples, to a full build of the same registries.
//
// A view is patched only when it was built from exactly the inputs it is read at now: the same view
// configuration (its fingerprint), the same trusted keys (the slot), and registries that only grew (the
// generation of each is what it was). Anything else, and any error on the way, is a full build.

// Counters of how views were made, for a test that a step took the path it should.
var (
	fullBuilds, patchBuilds, quadGraphsRead atomic.Int64
	lastFallback                            atomic.Value // string: why the last patch gave way to a full build
)

// builtInfo is what a view is marked with besides its key: the arrival number and generation of each of
// its registries at the time it was built, and the fingerprint of the configuration it was built under.
type builtInfo struct {
	regs                 []string
	arrivals, generation map[string]int
	fingerprint          string
}

// counters reads, for each registry, how many records it has brought (the arrival counter) and how many
// times a read found it had lost some (its generation).
func (d *Daemon) counters(ctx context.Context, regs []string) (arrivals, gens map[string]int, err error) {
	vals := make([]string, len(regs))
	for i, r := range regs {
		vals[i] = "<" + r + ">"
	}
	rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT ?r ?p ?n WHERE { GRAPH <%s> {
  VALUES ?r { %s } ?r ?p ?n FILTER(?p IN (<%sarrivals>, <%sgeneration>)) } }`, indexGraph, strings.Join(vals, " "), kNS, kNS)}})
	if err != nil {
		return nil, nil, err
	}
	arrivals, gens = map[string]int{}, map[string]int{}
	for _, r := range rows {
		n, _ := strconv.Atoi(r["n"])
		if r["p"] == kNS+"arrivals" {
			arrivals[r["r"]] = n
		} else {
			gens[r["r"]] = n
		}
	}
	return arrivals, gens, nil
}

// viewMark is what slot's mark graph says of its view.
type viewMark struct {
	key, defHash, fingerprint string
	builtAt, builtGen         map[string]int
}

func (d *Daemon) readMark(ctx context.Context, slot string) (viewMark, error) {
	rows, err := d.selectTerms(ctx, "SELECT ?s ?p ?o WHERE { GRAPH <"+markIRI(slot)+"> { ?s ?p ?o } }")
	if err != nil {
		return viewMark{}, err
	}
	m := viewMark{builtAt: map[string]int{}, builtGen: map[string]int{}}
	for _, r := range rows {
		s, p, o := r["s"].Value, r["p"].Value, r["o"].Value
		switch {
		case s == viewIRI(slot) && p == kNS+"key":
			m.key = o
		case s == viewIRI(slot) && p == kNS+"defHash":
			m.defHash = o
		case s == viewIRI(slot) && p == kNS+"fingerprint":
			m.fingerprint = o
		case p == kNS+"builtAt":
			m.builtAt[s], _ = strconv.Atoi(o)
		case p == kNS+"builtGen":
			m.builtGen[s], _ = strconv.Atoi(o)
		}
	}
	return m, nil
}

// markTriples are the lines a mark carries for a view built at b (beside its key and hashes).
func (b builtInfo) markTriples(slot string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "<%s> <%sfingerprint> %s .\n", viewIRI(slot), kNS, strconv.Quote(b.fingerprint))
	for _, r := range b.regs {
		fmt.Fprintf(&s, "<%s> <%sbuiltAt> \"%d\"^^<%s> .\n<%s> <%sbuiltGen> \"%d\"^^<%s> .\n",
			r, kNS, b.arrivals[r], xsdInteger, r, kNS, b.generation[r], xsdInteger)
	}
	return s.String()
}

func (d *Daemon) selectTerms(ctx context.Context, q string) ([]map[string]Term, error) {
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	res, err := ParseResults(body)
	if err != nil {
		return nil, err
	}
	return res.Terms, nil
}

// inChunks calls f with xs in pieces a query can carry.
func inChunks(xs []string, f func([]string) error) error {
	const chunk = 2000
	for i := 0; i < len(xs); i += chunk {
		if err := f(xs[i:min(i+chunk, len(xs))]); err != nil {
			return err
		}
	}
	return nil
}

func iriList(prefix string, xs []string) string {
	var b strings.Builder
	for _, x := range xs {
		b.WriteString(" <" + prefix + x + ">")
	}
	return b.String()
}

func setList(s map[string]bool) []string { return sortedKeys(s) }

// chainRow is a claim's place in its chain as the chain graph (or the links graph) says.
type chainRow struct {
	scope, prev string
	genesis     bool
	pos         int // -1 unless the chain graph holds it
	any         bool
}

// rowsIn reads the chain rows of the claims hexes from graph g: the chain graph (resolved claims, with
// positions) or the links graph (every record's own links, no positions).
func (d *Daemon) rowsIn(ctx context.Context, g string, hexes []string) (map[string]chainRow, error) {
	out := map[string]chainRow{}
	err := inChunks(hexes, func(part []string) error {
		rows, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?s ?p ?o WHERE { GRAPH <%s> { VALUES ?s {%s } ?s ?p ?o } }`, g, iriList(oNS, part)))
		if err != nil {
			return err
		}
		for _, r := range rows {
			h := strings.TrimPrefix(r["s"].Value, oNS)
			row, ok := out[h]
			if !ok {
				row.pos = -1
			}
			// Only the claim's own links count: the chain graph also says, of a scope's seed, how many
			// claims wait for it (urn:kton:unresolved), which makes the seed no more resolved.
			switch r["p"].Value {
			case nkNS + "scope":
				row.scope, row.any = strings.TrimPrefix(r["o"].Value, oNS), true
			case nkNS + "prev":
				row.prev, row.any = strings.TrimPrefix(r["o"].Value, oNS), true
			case nkNS + "genesis":
				row.genesis, row.any = true, true
			case nkNS + "position":
				row.pos, _ = strconv.Atoi(r["o"].Value)
				row.any = true
			}
			out[h] = row
		}
		return nil
	})
	return out, err
}

// kernelResolved are those of hexes some of regs' kernels holds with their chain resolved.
func (d *Daemon) kernelResolved(ctx context.Context, regs, hexes []string) (map[string]bool, error) {
	out := map[string]bool{}
	rv := make([]string, len(regs))
	for i, r := range regs {
		rv[i] = "<" + r + ">"
	}
	err := inChunks(hexes, func(part []string) error {
		rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?c WHERE { GRAPH <%s> {
  VALUES ?r { %s } VALUES ?c {%s } ?r <%sresolves> ?c } }`, indexGraph, strings.Join(rv, " "), iriList(oNS, part), kNS)}})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[strings.TrimPrefix(r["c"], oNS)] = true
		}
		return nil
	})
	return out, err
}

// waiting are the claims some registry holds waiting that the view's chain graph does not hold yet.
func (d *Daemon) waiting(ctx context.Context, slot string, regs []string) ([]string, error) {
	rv := make([]string, len(regs))
	for i, r := range regs {
		rv[i] = "<" + r + ">"
	}
	rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?c WHERE { GRAPH <%s> {
  VALUES ?r { %s } ?r <%sdefers> ?c FILTER NOT EXISTS { GRAPH <%s> { ?c <%sscope> ?x } } } }`, indexGraph, strings.Join(rv, " "), kNS, chainIRI(slot), nkNS)}})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		out = append(out, strings.TrimPrefix(r["c"], oNS))
	}
	sort.Strings(out)
	return out, nil
}

// recGraphs are the graphs of one record a view's dataset takes: its own graphs and the attribution
// under keys the view trusts, where the registry that attests it also holds the record.
type recGraphs struct {
	layer string
	graph string
	attr  []string
}

// recordsOf reads recGraphs of the record graphs recs, as dataset does for all of them.
func (d *Daemon) recordsOf(ctx context.Context, regs []string, trusted map[string]bool, recs []string) (map[string]*recGraphs, error) {
	rv := make([]string, len(regs))
	for i, r := range regs {
		rv[i] = "<" + r + ">"
	}
	out := map[string]*recGraphs{}
	holds := map[[2]string]bool{}
	err := inChunks(recs, func(part []string) error {
		var vs strings.Builder
		for _, g := range part {
			vs.WriteString(" <" + g + ">")
		}
		held, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?reg ?rec ?layer WHERE { GRAPH <%s> {
  VALUES ?reg { %s } VALUES ?rec {%s } ?reg <%sholds> ?rec . ?rec <%slayer> ?layer } }`, indexGraph, strings.Join(rv, " "), vs.String(), kNS, kNS)}})
		if err != nil {
			return err
		}
		for _, r := range held {
			if out[r["rec"]] == nil {
				out[r["rec"]] = &recGraphs{layer: r["layer"], graph: r["rec"]}
			}
			holds[[2]string{r["reg"], r["rec"]}] = true
		}
		attested, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?reg ?rec ?ag ?kid WHERE { GRAPH <%s> {
  VALUES ?reg { %s } VALUES ?rec {%s } ?reg <%sattests> ?ag . ?ag <%sof> ?rec ; <%skeyid> ?kid } }`, indexGraph, strings.Join(rv, " "), vs.String(), kNS, kNS, kNS)}})
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, r := range attested {
			if x := out[r["rec"]]; x != nil && trusted[r["kid"]] && holds[[2]string{r["reg"], r["rec"]}] && !seen[r["ag"]] {
				seen[r["ag"]] = true
				x.attr = append(x.attr, r["ag"])
			}
		}
		return nil
	})
	for _, x := range out {
		sort.Strings(x.attr)
	}
	return out, err
}

// named are the nanopublication graphs and attribution graphs a claim with a trusted signature adds to
// the nanopublication dataset.
func (x *recGraphs) named() []string {
	return append(projected{layer: x.layer, graph: x.graph}.graphs(), x.attr...)
}

// presentIn tells which of qs graph g already holds.
func (d *Daemon) presentIn(ctx context.Context, g string, qs []Quad) (map[string]bool, error) {
	out := map[string]bool{}
	const chunk = 400
	for i := 0; i < len(qs); i += chunk {
		var vs strings.Builder
		for _, q := range qs[i:min(i+chunk, len(qs))] {
			vs.WriteString(" (" + ntTerm(q.S) + " " + ntTerm(q.P) + " " + ntTerm(q.O) + ")")
		}
		rows, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?s ?p ?o WHERE { VALUES (?s ?p ?o) {%s } GRAPH <%s> { ?s ?p ?o } }`, vs.String(), g))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[quadKey(Quad{S: r["s"], P: r["p"], O: r["o"]})] = true
		}
	}
	return out, nil
}

// quadKey identifies a triple: the same for a term read back from the store and one the projection wrote.
func quadKey(q Quad) string {
	n := func(t Term) string {
		if t.Datatype == xsdNS+"string" {
			t.Datatype = ""
		}
		return ntTerm(t)
	}
	return n(q.S) + " " + n(q.P) + " " + n(q.O)
}

// edits are the triples one patch deletes from and inserts into each graph, as sets.
type edits map[string]*graphEdit

type graphEdit struct{ del, ins map[string]Quad }

func (e edits) of(g string) *graphEdit {
	if e[g] == nil {
		e[g] = &graphEdit{del: map[string]Quad{}, ins: map[string]Quad{}}
	}
	return e[g]
}
func (e edits) insert(g string, q Quad) { q.G = ""; e.of(g).ins[quadKey(q)] = q }
func (e edits) remove(g string, q Quad) { q.G = ""; e.of(g).del[quadKey(q)] = q }

// settle drops what is both deleted and inserted: a triple that stays where it is moves nothing.
func (e edits) settle() {
	for g, x := range e {
		for k := range x.del {
			if _, both := x.ins[k]; both {
				delete(x.del, k)
				delete(x.ins, k)
			}
		}
		if len(x.del) == 0 && len(x.ins) == 0 {
			delete(e, g)
		}
	}
}

func sortedLines(m map[string]Quad) string {
	ks := sortedKeys(m)
	return strings.Join(ks, " .\n") + func() string {
		if len(ks) > 0 {
			return " .\n"
		}
		return ""
	}()
}

// nextState is a graph's state after an edit: the hash of the state before and of what was deleted and
// inserted (canonical, sorted), so it changes when the graph does and not otherwise (ADR-022 Q2).
func nextState(old string, x *graphEdit) string {
	return core.HashBytes([]byte(old + "\n-\n" + sortedLines(x.del) + "+\n" + sortedLines(x.ins)))
}

// readKept reads the dataset a view was kept under, whatever key the file names.
func (d *Daemon) readKept(slot string) (keptFile, bool) {
	var k keptFile
	if d.Dir == "" {
		return k, false
	}
	b, err := os.ReadFile(filepath.Join(d.datasetDir(), slot+".json"))
	if err != nil {
		return k, false
	}
	if json.Unmarshal(b, &k) != nil {
		return k, false
	}
	return k, true
}

// patchIn is what prepare knows once the registries are read.
type patchIn struct {
	slot, key string
	regs      []string // every registry of the slot
	inputs    []binaries.Input
	keys      []ed25519.PublicKey
	vc        ViewConfig
	built     builtInfo // the registries' counters as they are now
}

func (in patchIn) nektonRegs() []string {
	var out []string
	for _, i := range in.inputs {
		out = append(out, regIRI(i.NektonDir))
	}
	return out
}

// deltaRow is a graph one of the view's registries brought since the view was built.
type deltaRow struct{ layer, rec, kid string }

// patchView brings slot's view up to the registries' state by the delta alone. ok is false when the view
// has to be built whole; an error is the same, the caller never leaves a half patched view: everything
// the patch writes is one update.
func (d *Daemon) patchView(ctx context.Context, in patchIn) (ds *prepared, ok bool, err error) {
	slot := in.slot
	trusted := trustedIDs(in.keys)
	if d.Dir != "" {
		if err := os.MkdirAll(d.datasetDir(), 0o755); err != nil {
			return nil, false, err
		}
		unlock, err := flock.Lock(filepath.Join(d.datasetDir(), slot+".lock"))
		if err != nil {
			return nil, false, err
		}
		defer unlock()
	}
	mark, err := d.readMark(ctx, slot)
	if err != nil {
		return nil, false, err
	}
	if mark.key == in.key { // another process patched it while this one waited for the lock
		if ds, ok := d.keptDataset(ctx, slot, in.key); ok {
			return ds, true, nil
		}
		return nil, false, nil
	}
	if mark.key == "" || mark.fingerprint == "" || mark.fingerprint != in.built.fingerprint {
		return nil, false, nil
	}
	for _, r := range in.regs {
		at, hasAt := mark.builtAt[r]
		gen, hasGen := mark.builtGen[r]
		if !hasAt || !hasGen || gen != in.built.generation[r] || at > in.built.arrivals[r] {
			return nil, false, nil
		}
	}
	kept, haveKept := d.readKept(slot)
	if !haveKept || kept.Key != mark.key || mark.defHash == "" {
		return nil, false, nil
	}

	// What arrived: the graphs each registry numbered past the number the view was built at.
	delta := map[string]deltaRow{}
	settled := map[string]bool{} // claims a registry's kernel took out of waiting since
	for _, r := range in.regs {
		from, to := mark.builtAt[r], in.built.arrivals[r]
		if to <= from {
			continue
		}
		rows, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?g ?layer ?rec ?kid WHERE { GRAPH <%s> { ?g <%sarrived:%s> ?n FILTER(?n > %d && ?n <= %d)
  OPTIONAL { ?g <%slayer> ?layer } OPTIONAL { ?g <%sof> ?rec . ?g <%skeyid> ?kid } } }`,
			indexGraph, kNS, strings.TrimPrefix(r, kNS+"reg:"), from, to, kNS, kNS, kNS))
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			delta[row["g"].Value] = deltaRow{row["layer"].Value, row["rec"].Value, row["kid"].Value}
		}
		rows, err = d.selectTerms(ctx, fmt.Sprintf(`SELECT ?c WHERE { GRAPH <%s> { ?c <%ssettled:%s> ?n FILTER(?n > %d && ?n <= %d) } }`,
			indexGraph, kNS, strings.TrimPrefix(r, kNS+"reg:"), from, to))
		if err != nil {
			return nil, false, err
		}
		for _, row := range rows {
			settled[strings.TrimPrefix(row["c"].Value, oNS)] = true
		}
	}
	// New to the view: a graph a registry brought that no registry of the view had brought when it was
	// built (a record reaching a second registry, or one it already held, is nothing new).
	isNew := map[string]bool{}
	regOf := map[string]string{}
	for _, r := range in.regs {
		regOf[strings.TrimPrefix(r, kNS+"reg:")] = r
	}
	if err := inChunks(sortedKeys(delta), func(part []string) error {
		var vs strings.Builder
		for _, g := range part {
			vs.WriteString(" <" + g + ">")
			isNew[g] = true
		}
		rows, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?g ?p ?n WHERE { GRAPH <%s> { VALUES ?g {%s } ?g ?p ?n FILTER(STRSTARTS(STR(?p), "%sarrived:")) } }`, indexGraph, vs.String(), kNS))
		if err != nil {
			return err
		}
		for _, row := range rows {
			reg, mine := regOf[strings.TrimPrefix(row["p"].Value, kNS+"arrived:")]
			if n, _ := strconv.Atoi(row["n"].Value); mine && n <= mark.builtAt[reg] {
				isNew[row["g"].Value] = false
			}
		}
		return nil
	}); err != nil {
		return nil, false, err
	}

	var defAdd []string // graphs the view's record graph takes
	nektonNew, nektonAny := map[string]bool{}, map[string]bool{}
	attrNew := map[string]bool{} // claims gaining a trusted attribution
	for _, g := range sortedKeys(delta) {
		row := delta[g]
		if row.rec == "" { // a record graph
			if strings.HasPrefix(g, npNS) {
				h := claimHex(g)
				nektonAny[h] = true
				if isNew[g] {
					nektonNew[h] = true
				}
			} else if isNew[g] && row.layer == "plankton" {
				defAdd = append(defAdd, g)
			}
			continue
		}
		if !trusted[row.kid] || !isNew[g] {
			continue
		}
		if strings.HasPrefix(row.rec, npNS) {
			attrNew[claimHex(row.rec)] = true
		} else {
			defAdd = append(defAdd, g)
		}
	}

	// Which chains resolved. The kernel of each registry said so when it was read; of the claims every
	// registry holds waiting, the kernel's union of the slot's registries settles those whose seed or
	// predecessor arrived in the delta (decision 2 of the ADR's build: not on every patch).
	nektonRegs := in.nektonRegs()
	wait, err := d.waiting(ctx, slot, nektonRegs)
	if err != nil {
		return nil, false, err
	}
	hexes0 := map[string]bool{}
	for _, set := range []map[string]bool{nektonNew, nektonAny, attrNew, settled} {
		for h := range set {
			hexes0[h] = true
		}
	}
	for _, h := range wait {
		hexes0[h] = true
	}
	h0 := setList(hexes0)
	chain, err := d.rowsIn(ctx, chainIRI(slot), h0)
	if err != nil {
		return nil, false, err
	}
	links, err := d.rowsIn(ctx, linksGraph, h0)
	if err != nil {
		return nil, false, err
	}
	kRes, err := d.kernelResolved(ctx, nektonRegs, h0)
	if err != nil {
		return nil, false, err
	}
	// Arrived, for the union's question: in the delta, or held waiting until now and settled by a kernel.
	arrived := map[string]bool{}
	for h := range nektonAny {
		arrived[h] = true
	}
	for h := range settled {
		arrived[h] = true
	}
	var deferOnly []string
	for _, h := range wait {
		if kRes[h] {
			arrived[h] = true
		} else {
			deferOnly = append(deferOnly, h)
		}
	}
	unionRes := map[string]bool{}
	if len(deferOnly) > 0 {
		ask := false
		for _, h := range deferOnly {
			if arrived[h] || arrived[links[h].scope] || (links[h].prev != "" && arrived[links[h].prev]) {
				ask = true
				break
			}
		}
		if ask {
			var dirs []string
			for _, in := range in.inputs {
				if binaries.Exists(in.NektonDir) {
					dirs = append(dirs, in.NektonDir)
				}
			}
			if len(dirs) > 0 {
				u, err := nregistry.OpenUnion(dirs...)
				if err != nil {
					return nil, false, fmt.Errorf("settling the claims every registry holds waiting: %w", err)
				}
				for _, h := range deferOnly {
					if _, ok := u.Claim("sha256:" + h); ok {
						unionRes[h] = true
					}
				}
			}
		}
	}
	inChain := func(h string) bool { return chain[h].any }
	resolved := func(h string) bool { return inChain(h) || kRes[h] || unionRes[h] }
	admitted := map[string]bool{} // scoped claims whose chain resolved now and was not in the chain graph
	for _, h := range h0 {
		if !inChain(h) && resolved(h) && links[h].scope != "" {
			admitted[h] = true
		}
	}
	// A prev of an admitted claim that is in the chain graph already, for positions.
	need := map[string]bool{}
	for h := range admitted {
		if p := links[h].prev; p != "" && !admitted[p] && !chain[p].any {
			need[p] = true
		}
	}
	if len(need) > 0 {
		more, err := d.rowsIn(ctx, chainIRI(slot), setList(need))
		if err != nil {
			return nil, false, err
		}
		for h, r := range more {
			chain[h] = r
		}
	}
	posOf := map[string]int{}
	var pos func(h string, depth int) int
	pos = func(h string, depth int) int {
		if p, ok := posOf[h]; ok {
			return p
		}
		if r, ok := chain[h]; ok && r.any && !admitted[h] {
			return r.pos
		}
		l := links[h]
		switch {
		case !admitted[h] || depth > len(admitted)+1:
			return -1
		case l.genesis || l.scope == h:
			posOf[h] = 0
			return 0
		case l.prev == "":
			return -1
		}
		p := pos(l.prev, depth+1)
		if p >= 0 {
			p++
		}
		posOf[h] = p
		return p
	}
	for h := range admitted {
		pos(h, 0)
	}

	// The nanopublication dataset grows by the claims a trusted key signed that arrived or were co-signed,
	// whether or not their chain resolves: their record graphs are data like any imported graph.
	t0 := map[string]bool{}
	for h := range nektonNew {
		t0[h] = true
	}
	for h := range attrNew {
		t0[h] = true
	}
	for h := range admitted {
		t0[h] = true
	}
	recGraphOf := func(h string) string { return nanopub.RecordBase("sha256:" + h) }
	recIDs := func(set map[string]bool) []string {
		var out []string
		for _, h := range setList(set) {
			out = append(out, recGraphOf(h))
		}
		return out
	}
	recs, err := d.recordsOf(ctx, in.regs, trusted, recIDs(t0))
	if err != nil {
		return nil, false, err
	}
	var namedAdd []string
	tn := map[string]bool{} // touched claims that have a node: resolved, with a trusted signature
	for _, h := range setList(t0) {
		x := recs[recGraphOf(h)]
		if x == nil || len(x.attr) == 0 {
			continue
		}
		namedAdd = append(namedAdd, x.named()...)
		if resolved(h) {
			tn[h] = true
		}
	}

	// The neighbours (ADR-022 D1.2): the claims a touched claim replaces, whose nk:replacedBy it may
	// change, and every claim that replaces a claim whose node is taken, which its nk:replacedBy needs.
	graphsOf := func(set map[string]*recGraphs, hs map[string]bool) []string {
		var gs []string
		for h := range hs {
			if x := set[recGraphOf(h)]; x != nil && len(x.attr) > 0 {
				gs = append(gs, x.named()...)
			}
		}
		sort.Strings(gs)
		return gs
	}
	tq, err := d.quadsOf(ctx, graphsOf(recs, tn))
	if err != nil {
		return nil, false, err
	}
	out := map[string]bool{} // the claims whose nodes the patch takes
	for h := range tn {
		out[h] = true
	}
	for _, x := range replacesIn(tq) {
		out[x] = true
	}
	in2 := map[string]bool{}
	for h := range out {
		in2[h] = true
	}
	if len(out) > 0 {
		var xs []string
		for _, h := range setList(out) {
			xs = append(xs, oNS+h)
		}
		if err := inChunks(xs, func(part []string) error {
			var vs strings.Builder
			for _, x := range part {
				vs.WriteString(" <" + x + ">")
			}
			rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?r WHERE { GRAPH <%s> { VALUES ?x {%s } ?r <%sreplaces> ?x } }`, claimsIRI(slot), vs.String(), nkNS)}})
			if err != nil {
				return err
			}
			for _, r := range rows {
				in2[strings.TrimPrefix(r["r"], oNS)] = true
			}
			return nil
		}); err != nil {
			return nil, false, err
		}
	}
	// Whoever the input names, with its record read now when it was not read above.
	var extra []string
	for h := range in2 {
		if recs[recGraphOf(h)] == nil {
			extra = append(extra, recGraphOf(h))
		}
	}
	if len(extra) > 0 {
		more, err := d.recordsOf(ctx, in.regs, trusted, extra)
		if err != nil {
			return nil, false, err
		}
		for g, x := range more {
			recs[g] = x
		}
	}
	ci := map[string]bool{} // the input claims that are resolved and have a trusted signature
	for h := range in2 {
		if x := recs[recGraphOf(h)]; x != nil && len(x.attr) > 0 {
			ci[h] = true
		}
	}
	// A claim's own record may be resolved only through the chain graph (an old one).
	need = map[string]bool{}
	for h := range ci {
		if _, ok := chain[h]; !ok {
			need[h] = true
		}
	}
	if len(need) > 0 {
		more, err := d.rowsIn(ctx, chainIRI(slot), setList(need))
		if err != nil {
			return nil, false, err
		}
		for h, r := range more {
			chain[h] = r
		}
		kr, err := d.kernelResolved(ctx, nektonRegs, setList(need))
		if err != nil {
			return nil, false, err
		}
		for h := range kr {
			kRes[h] = true
		}
	}
	for h := range ci {
		if !resolved(h) {
			delete(ci, h)
			delete(out, h)
		}
	}
	quads, err := d.quadsOf(ctx, graphsOf(recs, ci))
	if err != nil {
		return nil, false, err
	}
	pl := map[string]chainLink{}
	pp := map[string]int{}
	for h := range ci {
		if admitted[h] {
			pl[h] = chainLink{links[h].scope, links[h].prev}
			pp[h] = posOf[h]
		} else if r := chain[h]; r.any {
			pl[h] = chainLink{r.scope, r.prev}
			pp[h] = r.pos
		}
	}
	view, spans := projectClaimsSpans(quads, pl, pp, func(h string) bool { return ci[h] }, in.vc)

	// What each claim whose node is taken was before the patch.
	had := map[string]bool{}
	if len(out) > 0 {
		var xs []string
		for _, h := range setList(out) {
			xs = append(xs, oNS+h)
		}
		if err := inChunks(xs, func(part []string) error {
			var vs strings.Builder
			for _, x := range part {
				vs.WriteString(" <" + x + ">")
			}
			rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?c WHERE { GRAPH <%s> { VALUES ?c {%s } ?c a <%sClaim> } }`, claimsIRI(slot), vs.String(), nkNS)}})
			if err != nil {
				return err
			}
			for _, r := range rows {
				had[strings.TrimPrefix(r["c"], oNS)] = true
			}
			return nil
		}); err != nil {
			return nil, false, err
		}
	}

	e := edits{}
	graphOfScope := func(g string) string {
		if g == Unscoped {
			return unscopedIRI(slot)
		}
		return scopeGraphIRI(slot, g)
	}
	born := map[string]string{} // claims that get a node now, by the scope graph they are in
	oldNodes := map[string][]string{}
	maxEnd := 0
	for _, sp := range spans {
		maxEnd = max(maxEnd, sp.end)
	}
	sharedIdx := map[int]bool{}
	for _, sp := range spans {
		for _, i := range sp.shared {
			sharedIdx[i] = true
		}
	}
	var restIns []Quad
	for _, h := range setList(out) {
		sp, ok := spans[h]
		if !ok {
			continue
		}
		g := ""
		for i := sp.start; i < sp.end; i++ {
			if !sharedIdx[i] {
				g = view[i].G
				break
			}
		}
		sg := graphOfScope(g)
		isBorn := !had[h]
		if isBorn {
			born[h] = g
		} else {
			oldNodes[sg] = append(oldNodes[sg], oNS+h)
		}
		for i := sp.start; i < sp.end; i++ {
			q := view[i]
			switch {
			case sharedIdx[i]:
				if isBorn {
					restIns = append(restIns, q)
				}
			case q.S.Value == oNS+h:
				e.insert(sg, q)
			case isBorn:
				e.insert(sg, q)
			}
		}
	}
	for _, sg := range sortedKeys(oldNodes) {
		subs := oldNodes[sg]
		if err := inChunks(subs, func(part []string) error {
			var vs strings.Builder
			for _, x := range part {
				vs.WriteString(" <" + x + ">")
			}
			rows, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?s ?p ?o WHERE { GRAPH <%s> { VALUES ?s {%s } ?s ?p ?o } }`, sg, vs.String()))
			if err != nil {
				return err
			}
			for _, r := range rows {
				e.remove(sg, Quad{S: r["s"], P: r["p"], O: r["o"]})
			}
			return nil
		}); err != nil {
			return nil, false, err
		}
	}
	for i := maxEnd; i < len(view); i++ { // signer labels
		if view[i].G == "" {
			restIns = append(restIns, view[i])
		}
	}
	restG := restIRI(slot)
	if len(restIns) > 0 {
		have, err := d.presentIn(ctx, restG, restIns)
		if err != nil {
			return nil, false, err
		}
		for _, q := range restIns {
			if !have[quadKey(q)] {
				e.insert(restG, q)
			}
		}
	}

	// The chain graph: each admitted claim's place, and the claims still waiting, by scope.
	chainG := chainIRI(slot)
	for _, h := range setList(admitted) {
		l, n := links[h], oNS+h
		e.insert(chainG, triple(n, nkNS+"scope", iri(oNS+l.scope)))
		if l.prev != "" {
			e.insert(chainG, triple(n, nkNS+"prev", iri(oNS+l.prev)))
		}
		if l.genesis {
			e.insert(chainG, triple(n, nkNS+"genesis", typed("true", xsdNS+"boolean")))
		}
		if p := posOf[h]; p >= 0 {
			e.insert(chainG, triple(n, nkNS+"position", typed(strconv.Itoa(p), xsdInteger)))
		}
	}
	remaining := map[string]int{}
	for _, h := range deferOnly {
		if !unionRes[h] && links[h].scope != "" {
			remaining[links[h].scope]++
		}
	}
	oldCounts, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?s ?o WHERE { GRAPH <%s> { ?s <%sunresolved> ?o } }`, chainG, kNS))
	if err != nil {
		return nil, false, err
	}
	for _, r := range oldCounts {
		e.remove(chainG, Quad{S: r["s"], P: iri(kNS + "unresolved"), O: r["o"]})
	}
	for s, n := range remaining {
		e.insert(chainG, triple(oNS+s, kNS+"unresolved", typed(strconv.Itoa(n), xsdInteger)))
	}

	// Scope metadata (in the rest graph): the count of a scope that took claims, a new seed's label and
	// parent, and the sources of every scope whose seed a registry brought (a second registry's copy
	// of a seed adds its name).
	seedsIn := map[string]bool{}
	for h := range nektonAny {
		if links[h].genesis {
			seedsIn[h] = true
		}
	}
	affected := map[string]bool{}
	bornCount := map[string]int{}
	newLabel, newParent := map[string]Term{}, map[string]Term{}
	for h, g := range born {
		affected[g] = true
		if g != h { // a scope's seed is in its graph and is not one of its claims
			bornCount[g]++
		}
		sp := spans[h]
		for i := sp.start; i < sp.end; i++ {
			q := view[i]
			if sharedIdx[i] || q.S.Value != oNS+h || g != h {
				continue
			}
			switch q.P.Value {
			case rdfsLbl:
				newLabel[g] = q.O
			case nkNS + "parent":
				newParent[g] = q.O
			}
		}
	}
	for h := range seedsIn {
		affected[h] = true
	}
	described := map[string]ClaimGraph{}
	for _, g := range sortedKeys(affected) {
		if g == "" {
			continue
		}
		mg := graphOfScope(g)
		old, err := d.selectTerms(ctx, fmt.Sprintf(`SELECT ?p ?o WHERE { GRAPH <%s> { <%s> ?p ?o } }`, restG, mg))
		if err != nil {
			return nil, false, err
		}
		var oldQ []Quad
		for _, r := range old {
			oldQ = append(oldQ, Quad{S: iri(mg), P: r["p"], O: r["o"]})
		}
		exists := len(oldQ) > 0
		if !exists {
			// A scope with no node yet has no graph and no metadata (a seed alone is a node).
			hasNode := false
			for _, bg := range born {
				hasNode = hasNode || bg == g
			}
			if !hasNode {
				continue
			}
		}
		claims := 0
		var label, parent *Term
		var srcs []string
		for _, q := range oldQ {
			switch q.P.Value {
			case nkNS + "claims":
				claims, _ = strconv.Atoi(q.O.Value)
			case rdfsLbl:
				t := q.O
				label = &t
			case nkNS + "parent":
				t := q.O
				parent = &t
			case nkNS + "source":
				srcs = append(srcs, q.O.Value)
			}
		}
		claims += bornCount[g]
		if t, ok := newLabel[g]; ok {
			label = &t
		}
		if t, ok := newParent[g]; ok {
			parent = &t
		}
		if g != Unscoped { // always, in the order of the inputs, as a full build names them
			if srcs, err = d.sourcesOf(ctx, in, g); err != nil {
				return nil, false, err
			}
		}
		var want []Quad
		want = append(want, triple(mg, rdfType, iri(nkNS+"ClaimGraph")))
		if g == Unscoped {
			want = append(want, triple(mg, nkNS+"unscoped", typed("true", xsdNS+"boolean")))
		} else {
			want = append(want, triple(mg, nkNS+"scope", iri(oNS+g)))
		}
		want = append(want, triple(mg, nkNS+"claims", typed(strconv.Itoa(claims), xsdNS+"integer")))
		if label != nil {
			want = append(want, triple(mg, rdfsLbl, *label))
		}
		if parent != nil {
			want = append(want, triple(mg, nkNS+"parent", *parent))
		}
		for _, s := range srcs {
			want = append(want, triple(mg, nkNS+"source", lit(s)))
		}
		have := map[string]bool{}
		for _, q := range want {
			have[quadKey(q)] = true
		}
		for _, q := range oldQ {
			if !have[quadKey(q)] {
				e.remove(restG, q)
			}
		}
		for _, q := range want {
			e.insert(restG, q)
		}
		described[mg] = describeGraphs(want)[0]
	}
	e.settle()

	// One update: the plankton records into the view's record graph, each graph's edit (mirrored into the
	// union of the claims), and the mark with the states the graphs have now.
	v, c, m := "<"+viewIRI(slot)+">", "<"+claimsIRI(slot)+">", "<"+markIRI(slot)+">"
	var u strings.Builder
	defHash := mark.defHash
	if len(defAdd) > 0 {
		sort.Strings(defAdd)
		u.WriteString("INSERT { GRAPH " + v + " { ?s ?p ?o } } WHERE { VALUES ?g {")
		for _, g := range defAdd {
			u.WriteString(" <" + g + ">")
		}
		u.WriteString(" } GRAPH ?g { ?s ?p ?o } } ;\n")
		defHash = core.HashBytes([]byte(mark.defHash + "\n" + strings.Join(defAdd, "\n")))
	}
	oldHash := map[string]string{}
	hashRows, err := d.selectTerms(ctx, "SELECT ?g ?h WHERE { GRAPH "+m+" { ?g <"+kNS+"hash> ?h } }")
	if err != nil {
		return nil, false, err
	}
	for _, r := range hashRows {
		oldHash[r["g"].Value] = r["h"].Value
	}
	newHash := map[string]string{}
	for _, g := range sortedKeys(e) {
		x := e[g]
		mirror := g != chainG
		write := func(verb string, set map[string]Quad) {
			if len(set) == 0 {
				return
			}
			body := sortedLines(set)
			u.WriteString(verb + " DATA { GRAPH <" + g + "> {\n" + body + "}\n")
			if mirror {
				u.WriteString("GRAPH " + c + " {\n" + body + "}\n")
			}
			u.WriteString("} ;\n")
		}
		write("DELETE", x.del)
		write("INSERT", x.ins)
		newHash[g] = nextState(oldHash[g], x)
	}
	u.WriteString("DELETE WHERE { GRAPH " + m + " { " + v + " <" + kNS + "key> ?x } } ;\n")
	u.WriteString("DELETE WHERE { GRAPH " + m + " { " + v + " <" + kNS + "defHash> ?x } } ;\n")
	for _, r := range in.regs {
		u.WriteString("DELETE WHERE { GRAPH " + m + " { <" + r + "> <" + kNS + "builtAt> ?x } } ;\n")
	}
	for _, g := range sortedKeys(newHash) {
		u.WriteString("DELETE WHERE { GRAPH " + m + " { <" + g + "> <" + kNS + "hash> ?x } } ;\n")
	}
	u.WriteString("INSERT DATA { GRAPH " + m + " { " + v + " <" + kNS + "key> " + strconv.Quote(in.key) +
		" ; <" + kNS + "defHash> " + strconv.Quote(defHash) + " .\n")
	for _, r := range in.regs {
		fmt.Fprintf(&u, "<%s> <%sbuiltAt> \"%d\"^^<%s> .\n", r, kNS, in.built.arrivals[r], xsdInteger)
	}
	for _, g := range sortedKeys(newHash) {
		u.WriteString("<" + g + "> <" + kNS + "hash> " + strconv.Quote(newHash[g]) + " .\n")
	}
	u.WriteString("} }")
	if err := d.update(ctx, []byte(u.String())); err != nil {
		return nil, false, err
	}

	// The dataset as kept: the nanopublication graphs and scope graphs it names, and what describes them.
	named := map[string]bool{}
	for _, g := range kept.Named {
		named[g] = true
	}
	for _, g := range namedAdd {
		named[g] = true
	}
	scopes := map[string]bool{}
	for _, g := range kept.Scopes {
		scopes[g] = true
	}
	for g := range e {
		if g != restG && g != chainG {
			scopes[g] = true
		}
	}
	graphs := map[string]ClaimGraph{}
	var order []string
	for _, cg := range kept.Graphs {
		k := scopeKey(slot, cg)
		graphs[k] = cg
		order = append(order, k)
	}
	for _, cg := range described {
		k := scopeKey(slot, cg)
		if _, had := graphs[k]; !had {
			order = append(order, k)
		}
		graphs[k] = cg
	}
	sort.Strings(order)
	var list []ClaimGraph
	for i, k := range order {
		if i > 0 && order[i-1] == k {
			continue
		}
		list = append(list, graphs[k])
	}
	ds = &prepared{def: []string{viewIRI(slot)}, claims: []string{viewIRI(slot), claimsIRI(slot)},
		named: sortedKeys(named), scopes: sortedKeys(scopes), graphs: list, slot: slot, registries: len(in.regs)}
	return ds, true, nil
}

// scopeKey orders a view's claim graphs as a full build lists them: the unscoped graph first, then the
// scope graphs by their seed's hex.
func scopeKey(slot string, g ClaimGraph) string {
	if g.Unscoped {
		return Unscoped
	}
	return strings.TrimPrefix(g.Seed, oNS)
}

// replacesIn reads, from the quads of claims' nanopublication graphs, the claims each names in its
// replaces field (lower case hex), as ClaimView does.
func replacesIn(qs []Quad) []string {
	var out []string
	for _, q := range qs {
		if !isClaimGraph(q.G, npAssert) || q.P.Value != nkNS+"replaces" {
			continue
		}
		if q.S.Value != npNS+claimHex(q.G)+".#o" {
			continue
		}
		if x := strings.ToLower(claimHex(q.O.Value)); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// sourcesOf names, in the order the inputs are joined, the inputs whose nekton registry holds the seed
// of scope g, as binaries.ClaimSources does from the registries' records.
func (d *Daemon) sourcesOf(ctx context.Context, in patchIn, seed string) ([]string, error) {
	base := nanopub.RecordBase("sha256:" + seed)
	var names []string
	for _, input := range in.inputs {
		rows, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT ?r WHERE { GRAPH <%s> { <%s> <%sholds> <%s> . BIND(1 AS ?r) } }`,
			indexGraph, regIRI(input.NektonDir), kNS, base)}})
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		name := input.Name
		if name == binaries.Own {
			name = "own"
		}
		if len(names) == 0 || names[len(names)-1] != name {
			names = append(names, name)
		}
	}
	return names, nil
}
