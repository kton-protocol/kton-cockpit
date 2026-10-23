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
	def, named []string
	registries int
	kept       bool
}

// prepare reads into the store whatever changed in the registries of cfg's union and chooses the
// dataset a query under keys reads.
func (d *Daemon) prepare(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey) (*prepared, error) {
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, err
	}
	key := datasetKey(inputs, keys)
	if ds, ok := d.keptDataset(key); ok {
		return ds, nil
	}
	kept := true
	var regs []string
	for _, in := range inputs {
		for _, r := range []struct{ layer, dir string }{{"plankton", in.PlanktonDir}, {"nekton", in.NektonDir}} {
			changed, err := d.apply(ctx, r.layer, r.dir, keys)
			if err != nil {
				return nil, fmt.Errorf("reading %s registry %s into the index: %w", r.layer, r.dir, err)
			}
			kept = kept && !changed
			regs = append(regs, regIRI(r.dir))
		}
	}
	def, named, err := d.dataset(ctx, regs, trustedIDs(keys))
	if err != nil {
		return nil, err
	}
	ds := &prepared{def: def, named: named, registries: len(regs), kept: kept}
	d.keepDataset(key, ds)
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
	keys     map[string]bool // public keys, hex
}

func (d *Daemon) state(ctx context.Context, reg string) (regState, error) {
	q := fmt.Sprintf(`SELECT ?p ?o WHERE { GRAPH <%s> { <%s> ?p ?o FILTER(?p IN (<%sposition>, <%scursor>, <%skey>)) } }`,
		indexGraph, reg, kNS, kNS, kNS)
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
		if s.position != now {
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
	if s.position == "" || last.Epoch != pos.Epoch || (last.Objects != pos.Objects && last.MaxSeq == pos.MaxSeq) || newKey {
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
			ps = projectNekton(r.Records(since), keys)
			cursor = min(r.MaxSeq(), pos.MaxSeq)
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
	heldNow, attestsNow := map[string]bool{}, map[string]bool{}
	for _, p := range ps {
		recs.Write(p.own)
		m("<"+reg+">", "holds", "<"+p.graph+">")
		m("<"+p.graph+">", "layer", nqLit(p.layer))
		heldNow[p.graph] = true
		for kid, a := range p.attr {
			recs.Write(a)
			g := p.attributionGraph(kid)
			// Attribution belongs to the registry that holds the signature: another registry's copy
			// of the same record may carry fewer signatures, and must not be attributed by this one.
			m("<"+reg+">", "attests", "<"+g+">")
			m("<"+g+">", "of", "<"+p.graph+">")
			m("<"+g+">", "keyid", nqLit(kid))
			attestsNow[g] = true
		}
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
	done := fmt.Sprintf(`DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]sposition> ?a } } ;
DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]scursor> ?b } } ;
DELETE WHERE { GRAPH <%[1]s> { <%[2]s> <%[3]skey> ?c } } ;
INSERT DATA { GRAPH <%[1]s> { %[4]s } }`, indexGraph, reg, kNS, mark.String())
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
	var gone strings.Builder
	for _, r := range rows {
		switch {
		case r["p"] == kNS+"holds" && !held[r["o"]]:
		case r["p"] == kNS+"attests" && !attests[r["o"]] && used[r["kid"]]:
		default:
			continue
		}
		fmt.Fprintf(&gone, "<%s> <%s> <%s> .\n", reg, r["p"], r["o"])
	}
	if gone.Len() == 0 {
		return nil
	}
	u := fmt.Sprintf(`DELETE DATA { GRAPH <%s> { %s } }`, indexGraph, gone.String())
	_, err = d.post(ctx, "/update", "", url.Values{"update": {u}})
	return err
}

// dataset is the default and the named graphs a query of these registries reads under trusted.
func (d *Daemon) dataset(ctx context.Context, regs []string, trusted map[string]bool) (def, named []string, err error) {
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
		return nil, nil, err
	}
	attested, err := d.selectCSV(ctx, url.Values{"query": {fmt.Sprintf(`SELECT DISTINCT ?reg ?rec ?ag ?kid WHERE { GRAPH <%s> {
  VALUES ?reg { %s } ?reg <%sattests> ?ag . ?ag <%sof> ?rec ; <%skeyid> ?kid } }`, indexGraph, strings.Join(vals, " "), kNS, kNS, kNS)}})
	if err != nil {
		return nil, nil, err
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
	}
	sort.Strings(def)
	sort.Strings(named)
	return def, named, nil
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
