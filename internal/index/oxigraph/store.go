package oxigraph

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kton-protocol/kton-cockpit/internal/flock"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// The store holds every record once, in the kernel's own graphs (project.go), whichever
// repositories read it, and one attribution graph per key that verifies it. Trust is never stored:
// a query picks the attribution of the keys its configuration trusts.
//
// The graph urn:kton:index says what is in the store, per registry directory:
//   - <urn:kton:reg:<id>> urn:kton:holds <record graph>, for every record of that registry;
//   - <record graph> urn:kton:layer "plankton"|"nekton";
//   - <urn:kton:reg:<id>> urn:kton:attests <attribution graph>, for every signature of a key it was
//     attributed under that verifies in its own copy of the record (another registry's copy may
//     carry other signatures); <attribution graph> urn:kton:of <record graph> ; urn:kton:keyid "<keyid>";
//   - the registry's urn:kton:key (every public key it was attributed under), urn:kton:cursor (the
//     last position read with Records) and urn:kton:position (kton#233, read before the records),
//     replaced together last: a registry whose position differs is stale and is read again.
//
// ADR-022 adds, per registry, what arrived and what its kernel said of each chain (chainview.go):
// urn:kton:arrivals (the registry's own counter, replaced with the mark), urn:kton:generation, and
// <urn:kton:reg:<id>> urn:kton:resolves|defers <claim>.
//
// A registry is brought up to date before a query that reads it. Its position equal: nothing to do.
// Otherwise it is opened and Records(cursor) applied, or Records(0) when its epoch changed, when
// objects changed but nothing was numbered (records a git merge brought), or when the query trusts
// a key the registry was not attributed under yet.

const (
	indexGraph = "urn:kton:index"
	kNS        = "urn:kton:"
)

// Answer is a query's rows and how the store was read for it.
type Answer struct {
	Rows       []map[string]string
	Registries int  // registries the query read
	Kept       bool // every one of them was current; nothing was read into the store for this query
}

// Query answers a SPARQL SELECT over the union of cfg, under keys, reading into the store first
// whatever changed in its registries.
func Query(ctx context.Context, d *Daemon, cfg *config.Config, keys []ed25519.PublicKey, query string) (*Answer, error) {
	// The dataset of the request is the only wall between repositories; SERVICE goes around it.
	if err := index.CheckQuery(query); err != nil {
		return nil, err
	}
	ds, err := d.prepare(ctx, cfg, keys)
	if err != nil {
		return nil, err
	}
	rows, err := d.selectOn(ctx, ds.def, ds.named, query)
	if err != nil {
		return nil, err
	}
	return &Answer{Rows: rows, Registries: ds.registries, Kept: ds.kept}, nil
}

// prepared is the dataset of one query: its graphs, and how the registries behind them were read.
type prepared struct {
	// def and named are the kernel's projection (records once, claims as nanopublications); claims
	// is the default graph of the query projection (ADR-013): the records and the claims as nodes.
	def, named, claims []string
	// scopes are the view's claim graphs, one per scope and one for the unscoped claims (ADR-018): the
	// named graphs of a claims-dataset query, beside the repository's loaded graphs.
	scopes     []string
	graphs     []ClaimGraph // what the scope graphs are, for the answer
	slot       string       // the view: one per set of registries, trusted keys and templates
	registries int
	kept       bool
}

// prepare reads into the store whatever changed in the registries of cfg's union and chooses the
// dataset a query under keys reads.
// duringBuild runs once a build has marked itself, for a test.
var duringBuild func()

func (d *Daemon) prepare(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey) (*prepared, error) {
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, err
	}
	vc, err := ViewConfigFor(cfg)
	if err != nil {
		return nil, err
	}
	slot, key := datasetKey(inputs, keys, vc.Fingerprint())
	if ds, ok := d.keptDataset(ctx, slot, key); ok {
		_ = d.noteRead(ctx, slot, cfg.RepoRoot, inputs, readClock())
		return ds, nil
	}
	building := markBuilding(d.Dir, slot, cfg.RepoRoot)
	defer building.done()
	if duringBuild != nil {
		duringBuild()
	}
	kept := true
	var regs, nektonDirs []string
	for _, in := range inputs {
		nektonDirs = append(nektonDirs, in.NektonDir)
		for _, r := range []struct{ layer, dir string }{{"plankton", in.PlanktonDir}, {"nekton", in.NektonDir}} {
			changed, err := d.apply(ctx, r.layer, r.dir, keys)
			if err != nil {
				return nil, fmt.Errorf("reading %s registry %s into the index: %w", r.layer, r.dir, err)
			}
			kept = kept && !changed
			regs = append(regs, regIRI(r.dir))
		}
	}
	def, named, _, err := d.dataset(ctx, regs, trustedIDs(keys))
	if err != nil {
		return nil, err
	}
	building.phase("projecting the claims")
	ci, err := d.chainOf(ctx, regs, nektonDirs)
	if err != nil {
		return nil, fmt.Errorf("reading which chains resolve: %w", err)
	}
	view, err := d.claimView(ctx, named, ci, vc)
	if err != nil {
		return nil, fmt.Errorf("building the query projection of the claims: %w", err)
	}
	sources, err := binaries.ClaimSources(inputs)
	if err != nil {
		return nil, err
	}
	graphs, meta := scopeGraphs(slot, view, sources)
	building.phase("writing the view")
	if err := d.makeView(ctx, slot, key, def, append(view, meta...), graphs, ci.quads()); err != nil {
		return nil, fmt.Errorf("writing the index's view of the trusted records: %w", err)
	}
	ds := &prepared{def: []string{viewIRI(slot)}, claims: []string{viewIRI(slot), claimsIRI(slot)},
		named: named, scopes: sortedKeys(graphs), graphs: describeGraphs(meta), slot: slot, registries: len(regs), kept: kept}
	d.keepDataset(slot, key, ds)
	_ = d.noteRead(ctx, slot, cfg.RepoRoot, inputs, readClock())
	return ds, nil
}

// regIRI names a registry directory. The index belongs to one machine, so its path names it.
func regIRI(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	h := sha256.Sum256([]byte(abs))
	return kNS + "reg:" + hex.EncodeToString(h[:12])
}

type regState struct {
	position string // core.Position as JSON; "" when the registry was never read, or is being read
	cursor   int
	arrivals int             // the registry's own counter of what arrived (ADR-022)
	numbered bool            // arrivals was written: a registry read before ADR-022 is read again
	keys     map[string]bool // public keys, hex
}

func (d *Daemon) state(ctx context.Context, reg string) (regState, error) {
	q := fmt.Sprintf(`SELECT ?p ?o WHERE { GRAPH <%s> { <%s> ?p ?o FILTER(?p IN (<%sposition>, <%scursor>, <%skey>, <%sarrivals>)) } }`,
		indexGraph, reg, kNS, kNS, kNS, kNS)
	rows, err := d.selectCSV(ctx, url.Values{"query": {q}})
	if err != nil {
		return regState{}, err
	}
	s := regState{keys: map[string]bool{}}
	for _, r := range rows {
		switch r["p"] {
		case kNS + "position":
			s.position = r["o"]
		case kNS + "cursor":
			s.cursor, _ = strconv.Atoi(r["o"])
		case kNS + "key":
			s.keys[r["o"]] = true
		case kNS + "arrivals":
			s.arrivals, _ = strconv.Atoi(r["o"])
			s.numbered = true
		}
	}
	return s, nil
}

// apply brings one registry up to date and reports whether anything was read.
func (d *Daemon) apply(ctx context.Context, layer, dir string, trusted []ed25519.PublicKey) (bool, error) {
	reg := regIRI(dir)
	pos, err := core.ReadPosition(dir) // before the records, as kton#233 asks
	if err != nil {
		return false, err
	}
	pb, _ := json.Marshal(pos)
	now := string(pb)
	want := map[string]bool{}
	for _, k := range trusted {
		want[hex.EncodeToString(k)] = true
	}
	current := func(s regState) bool {
		if s.position != now || !s.numbered {
			return false
		}
		for k := range want {
			if !s.keys[k] {
				return false
			}
		}
		return true
	}
	s, err := d.state(ctx, reg)
	if err != nil || current(s) {
		return false, err
	}
	if d.Dir != "" { // two cockpits reading the same registry take turns; the second finds it current
		unlock, err := flock.Lock(filepath.Join(d.Dir, strings.TrimPrefix(reg, kNS+"reg:")+".lock"))
		if err != nil {
			return false, err
		}
		defer unlock()
		if s, err = d.state(ctx, reg); err != nil || current(s) {
			return false, err
		}
	}

	var last core.Position
	_ = json.Unmarshal([]byte(s.position), &last)
	since := s.cursor
	newKey := false
	for k := range want {
		newKey = newKey || !s.keys[k]
	}
	if s.position == "" || !s.numbered || last.Epoch != pos.Epoch || (last.Objects != pos.Objects && last.MaxSeq == pos.MaxSeq) || newKey {
		since = 0
	}
	var keys []ed25519.PublicKey
	all := map[string]bool{}
	for k := range s.keys {
		all[k] = true
	}
	for k := range want {
		all[k] = true
	}
	for k := range all {
		if b, err := hex.DecodeString(k); err == nil && len(b) == ed25519.PublicKeySize {
			keys = append(keys, ed25519.PublicKey(b))
		}
	}

	// The cursor is a position the store has written down, never one a read-only open handed out in
	// memory: records that arrived unnumbered get their lasting positions from the next writer, in its
	// own order, and a cursor past them would skip one that sorts earlier (kton-review V1 on PR #11).
	// Until a writer numbers them, objects differ while maxSeq does not, and they are read from 0.
	var ps []projected
	var chain []chainRecord // every record the registry returned, for the chain links (nekton)
	var said kernelSaid     // what its kernel says of those records and of those it held waiting before
	cursor := 0
	if _, err := os.Stat(dir); err == nil {
		switch layer {
		case "plankton":
			r, err := binaries.OpenPlanktonSource(dir)
			if err != nil {
				return false, err
			}
			if ps, err = projectPlankton(r, r.Records(since), keys); err != nil {
				return false, err
			}
			cursor = min(r.MaxSeq(), pos.MaxSeq)
		default:
			r, err := binaries.OpenNektonSource(dir)
			if err != nil {
				return false, err
			}
			recs := r.Records(since)
			ps = projectNekton(recs, keys)
			cursor = min(r.MaxSeq(), pos.MaxSeq)
			chain = chainRecords(recs)
			ids := make([]string, len(chain))
			for i, c := range chain {
				ids[i] = c.hex
			}
			// The kernel does not feed a claim again when it settles (its feed entry keeps the number it
			// had while it waited), so what this registry held waiting at the last read is asked by id.
			if since > 0 {
				before, err := d.previouslyDeferred(ctx, reg)
				if err != nil {
					return false, err
				}
				ids = append(ids, before...)
			}
			said = askKernel(r, ids)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}

	// Readers do not wait for the lock, and a full read can run while the position stays what it was
	// (a newly trusted key), so nothing a reader may select ever goes missing on the way: the new
	// records and attribution are added first, what the registry no longer holds is removed after,
	// and the position, cursor and keys are replaced together last. A registry half read is either
	// stale (its position not yet written) or a superset of what it was, never a subset.
	var recs, meta strings.Builder
	m := func(s, p, o string) { fmt.Fprintf(&meta, "%s <%s%s> %s <%s> .\n", s, kNS, p, o, indexGraph) }
	regID := strings.TrimPrefix(reg, kNS+"reg:")
	heldNow, attestsNow := map[string]bool{}, map[string]bool{}
	// What this registry brings for the first time is numbered, in the order it is read, from its own
	// counter: the delta of a view is what arrived past the number the view was built at (ADR-022 D1.1).
	var newHeld, newAttests []string
	for _, p := range ps {
		newHeld = append(newHeld, p.graph)
		for _, kid := range sortedKeys(p.attr) {
			newAttests = append(newAttests, p.attributionGraph(kid))
		}
	}
	fresh, err := d.unseen(ctx, reg, "holds", newHeld)
	if err != nil {
		return false, err
	}
	freshAttest, err := d.unseen(ctx, reg, "attests", newAttests)
	if err != nil {
		return false, err
	}
	arrivals := s.arrivals
	arrive := func(g string, isNew map[string]bool) {
		if isNew[g] {
			arrivals++
			delete(isNew, g) // a graph two records name is numbered once
			m("<"+g+">", "arrived:"+regID, fmt.Sprintf(`"%d"^^<%s>`, arrivals, xsdInteger))
		}
	}
	for _, p := range ps {
		recs.Write(p.own)
		m("<"+reg+">", "holds", "<"+p.graph+">")
		m("<"+p.graph+">", "layer", nqLit(p.layer))
		heldNow[p.graph] = true
		arrive(p.graph, fresh)
		for _, kid := range sortedKeys(p.attr) {
			recs.Write(p.attr[kid])
			g := p.attributionGraph(kid)
			// Attribution belongs to the registry that holds the signature: another registry's copy
			// of the same record may carry fewer signatures, and must not be attributed by this one.
			m("<"+reg+">", "attests", "<"+g+">")
			m("<"+g+">", "of", "<"+p.graph+">")
			m("<"+g+">", "keyid", nqLit(kid))
			attestsNow[g] = true
			arrive(g, freshAttest)
		}
	}
	links := linkNQuads(chain)
	if links != "" {
		meta.WriteString(links)
	}
	if recs.Len() > 0 {
		if err := d.load(ctx, "application/n-quads", recs.String()); err != nil {
			return false, fmt.Errorf("loading records: %w", err)
		}
	}
	if meta.Len() > 0 {
		if err := d.load(ctx, "application/n-quads", meta.String()); err != nil {
			return false, fmt.Errorf("recording what was loaded: %w", err)
		}
	}
	if since == 0 {
		if err := d.forgetDeparted(ctx, reg, heldNow, attestsNow, trustedIDs(keys)); err != nil {
			return false, err
		}
	}
	var mark strings.Builder
	for k := range all {
		fmt.Fprintf(&mark, "<%s> <%skey> %s .\n", reg, kNS, nqLit(k))
	}
	fmt.Fprintf(&mark, "<%s> <%scursor> %s .\n<%s> <%sposition> %s .\n", reg, kNS, nqLit(strconv.Itoa(cursor)), reg, kNS, nqLit(now))
	fmt.Fprintf(&mark, "<%s> <%sarrivals> \"%d\"^^<%s> .\n", reg, kNS, arrivals, xsdInteger)
	// What the registry's kernel said of its claims is replaced with the mark, in the same update: a
	// full read says it for every claim, so what it said before goes; a read from the cursor says it
	// for the claims it returned and for those that were waiting, each moving from one list to the other.
	var kernel strings.Builder
	if since == 0 {
		fmt.Fprintf(&kernel, "DELETE WHERE { GRAPH <%s> { <%s> <%sresolves> ?c } } ;\nDELETE WHERE { GRAPH <%s> { <%s> <%sdefers> ?c } } ;\n", indexGraph, reg, kNS, indexGraph, reg, kNS)
	}
	said.write(&kernel, reg, since == 0)
	done := fmt.Sprintf(`DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]sposition> ?a } } ;
DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]scursor> ?b } } ;
DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]skey> ?c } } ;
DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]sarrivals> ?d } } ;
%[5]sINSERT DATA { GRAPH <%[1]s> { %[4]s } }`, indexGraph, reg, kNS, mark.String(), kernel.String())
	if _, err := d.post(ctx, "/update", "", url.Values{"update": {done}}); err != nil {
		return false, fmt.Errorf("marking %s read: %w", reg, err)
	}
	return true, nil
}

// forgetDeparted removes, after a full read, what reg listed and no longer holds: records that left
// it, and its attribution under keys that no longer verify. Only the keys this read used are
// judged; attribution under a key another reader added meanwhile is that reader's to keep.
func (d *Daemon) forgetDeparted(ctx context.Context, reg string, held, attests, used map[string]bool) error {
	q := fmt.Sprintf(`SELECT ?p ?o ?kid WHERE { GRAPH <%s> { <%s> ?p ?o
  FILTER(?p IN (<%sholds>, <%sattests>)) OPTIONAL { ?o <%skeyid> ?kid } } }`, indexGraph, reg, kNS, kNS, kNS)
	rows, err := d.selectCSV(ctx, url.Values{"query": {q}})
	if err != nil {
		return err
	}
	var gone, arrived strings.Builder
	regID := strings.TrimPrefix(reg, kNS+"reg:")
	for _, r := range rows {
		switch {
		case r["p"] == kNS+"holds" && !held[r["o"]]:
		case r["p"] == kNS+"attests" && !attests[r["o"]] && used[r["kid"]]:
		default:
			continue
		}
		fmt.Fprintf(&gone, "<%s> <%s> <%s> .\n", reg, r["p"], r["o"])
		fmt.Fprintf(&arrived, "DELETE WHERE { GRAPH <%s> { <%s> <%sarrived:%s> ?n } } ;\n", indexGraph, r["o"], kNS, regID)
	}
	if gone.Len() == 0 {
		return nil
	}
	// A registry that lost records is one a view built from it cannot be patched against (ADR-022 D1.4):
	// the generation says so, and a view built at another generation is built whole.
	gen, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(
		`SELECT ?n WHERE { GRAPH <%s> { <%s> <%sgeneration> ?n } }`, indexGraph, reg, kNS)}})
	if err != nil {
		return err
	}
	n := 0
	if len(gen) > 0 {
		n, _ = strconv.Atoi(gen[0]["n"])
	}
	u := fmt.Sprintf(`DELETE DATA { GRAPH <%[1]s> { %[2]s } } ;
%[3]sDELETE WHERE { GRAPH <%[1]s> { <%[4]s> <%[5]sgeneration> ?g } } ;
INSERT DATA { GRAPH <%[1]s> { <%[4]s> <%[5]sgeneration> "%[6]d"^^<%[7]s> } }`,
		indexGraph, gone.String(), arrived.String(), reg, kNS, n+1, xsdInteger)
	_, err = d.post(ctx, "/update", "", url.Values{"update": {u}})
	return err
}

// dataset is the default and the named graphs a query of these registries reads under trusted.
func (d *Daemon) dataset(ctx context.Context, regs []string, trusted map[string]bool) (def, named, pubinfo []string, err error) {
	vals := make([]string, len(regs))
	for i, r := range regs {
		vals[i] = "<" + r + ">"
	}
	// Two queries joined here rather than one with an OPTIONAL: the engine evaluates that OPTIONAL
	// against every attestation of the registry for each record it holds, which on a union of a few
	// thousand records took 20 s where these take milliseconds.
	held, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?reg ?rec ?layer WHERE { GRAPH <%s> {
  VALUES ?reg { %s } ?reg <%sholds> ?rec . ?rec <%slayer> ?layer } }`, indexGraph, strings.Join(vals, " "), kNS, kNS)}})
	if err != nil {
		return nil, nil, nil, err
	}
	attested, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?reg ?rec ?ag ?kid WHERE { GRAPH <%s> {
  VALUES ?reg { %s } ?reg <%sattests> ?ag . ?ag <%sof> ?rec ; <%skeyid> ?kid } }`, indexGraph, strings.Join(vals, " "), kNS, kNS, kNS)}})
	if err != nil {
		return nil, nil, nil, err
	}
	type rec struct {
		layer string
		attr  []string
	}
	recs := map[string]*rec{}
	holds := map[[2]string]bool{}
	for _, r := range held {
		if recs[r["rec"]] == nil {
			recs[r["rec"]] = &rec{layer: r["layer"]}
		}
		holds[[2]string{r["reg"], r["rec"]}] = true
	}
	// Attribution is the registry's own: an attestation counts where the registry that attests
	// it also holds the record, as the single query joined them.
	seen := map[string]bool{}
	for _, r := range attested {
		if x := recs[r["rec"]]; x != nil && trusted[r["kid"]] && holds[[2]string{r["reg"], r["rec"]}] && !seen[r["ag"]] {
			seen[r["ag"]] = true
			x.attr = append(x.attr, r["ag"])
		}
	}
	for g, x := range recs {
		p := projected{layer: x.layer, graph: g}
		switch {
		case x.layer == "plankton":
			def = append(def, g)
			def = append(def, x.attr...)
		case len(x.attr) > 0: // a claim only when a trusted key signed it
			named = append(named, p.graphs()...)
			named = append(named, x.attr...)
		}
		if x.layer == "nekton" {
			pubinfo = append(pubinfo, g+"#pubinfo") // every claim: a chain counts all of them
		}
	}
	sort.Strings(def)
	sort.Strings(named)
	sort.Strings(pubinfo)
	return def, named, pubinfo, nil
}

// selectOn runs query with exactly def as its default graph and named as its named graphs.
func (d *Daemon) selectOn(ctx context.Context, def, named []string, query string) ([]map[string]string, error) {
	return d.selectCSV(ctx, datasetForm(def, named, query))
}

// datasetForm is a protocol request for query over the dataset of def and named.
func datasetForm(def, named []string, query string) url.Values {
	form := url.Values{"query": {query}}
	// An empty list would mean "the store's own", not "none": name one nobody uses.
	if len(def) == 0 {
		def = []string{kNS + "none"}
	}
	if len(named) == 0 {
		named = []string{kNS + "none"}
	}
	for _, g := range def {
		form.Add("default-graph-uri", g)
	}
	for _, g := range named {
		form.Add("named-graph-uri", g)
	}
	return form
}

func (d *Daemon) selectCSV(ctx context.Context, form url.Values) ([]map[string]string, error) {
	body, err := d.post(ctx, "/query", "text/csv", form)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading the index's answer: %w", err)
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
	return out, nil
}

// nqLit writes s as an N-Quads string literal.
func nqLit(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func (d *Daemon) post(ctx context.Context, path, accept string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return do(req)
}

func (d *Daemon) load(ctx context.Context, mediaType, data string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+"/store", strings.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mediaType)
	_, err = do(req)
	return err
}

func do(req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
