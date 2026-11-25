package oxigraph

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/flock"
)

// A query's dataset (the graphs a configuration's trusted keys may see) changes only when a
// registry moves or the keys change. It is kept on disk under exactly that, so a query against
// registries that did not move is the query alone: no reading in, no asking the store what it holds.
// The positions are read from the registries' own files, which is what reading in compares too.

// A query's default graph is one view graph per configuration, not the records' graphs listed one
// by one: the engine joins over a union of a few thousand default graphs graph by graph, which on a
// union of 24 registries made a four-pattern join run for minutes instead of a second. The view is
// the union copied once, when the dataset is computed, and marked with the dataset's key; a kept
// dataset is used only while its view still carries that mark.

// datasetKey names the configuration (slot: its registries and trusted keys, wherever they stand)
// and the dataset of these registries, at their current positions, under keys (key). The key is ""
// when a position cannot be read, and then nothing is kept.
func datasetKey(inputs []binaries.Input, keys []ed25519.PublicKey, view string) (slot, key string) {
	var regs, parts []string
	unread := false
	for _, in := range inputs {
		for _, dir := range []string{in.PlanktonDir, in.NektonDir} {
			regs = append(regs, regIRI(dir))
			pos, err := core.ReadPosition(dir)
			if err != nil {
				unread = true
				continue
			}
			b, _ := json.Marshal(pos)
			parts = append(parts, regIRI(dir)+"="+string(b))
		}
	}
	ids := make([]string, 0, len(keys))
	for k := range trustedIDs(keys) {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	slot = digest(append(regs, ids...))
	if unread {
		return slot, ""
	}
	return slot, digest(append(append(parts, ids...), "view="+view))
}

func digest(parts []string) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16])
}

func viewIRI(slot string) string   { return kNS + "view:" + slot }
func claimsIRI(slot string) string { return kNS + "view:" + slot + ":claims" }

// restIRI is a view's claims that belong to no scope: signer labels and the claim graphs' metadata.
func restIRI(slot string) string { return kNS + "view:" + slot + ":rest" }
func markIRI(slot string) string { return kNS + "view-of:" + slot }

func (d *Daemon) datasetDir() string { return filepath.Join(d.Dir, "datasets") }

type keptFile struct {
	Key        string
	Named      []string
	Scopes     []string
	Graphs     []ClaimGraph
	Registries int
}

func (d *Daemon) keptDataset(ctx context.Context, slot, key string) (*prepared, bool) {
	if d.Dir == "" || key == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(d.datasetDir(), slot+".json"))
	if err != nil {
		return nil, false
	}
	var k keptFile
	if json.Unmarshal(b, &k) != nil || k.Key != key || !d.viewIs(ctx, slot, key) {
		return nil, false
	}
	return &prepared{def: []string{viewIRI(slot)}, claims: []string{viewIRI(slot), claimsIRI(slot)},
		named: k.Named, scopes: k.Scopes, graphs: k.Graphs, slot: slot, registries: k.Registries, kept: true}, true
}

// viewIs tells whether slot's view holds the dataset key names; another process may have replaced it.
func (d *Daemon) viewIs(ctx context.Context, slot, key string) bool {
	q := "ASK { GRAPH <" + markIRI(slot) + "> { <" + viewIRI(slot) + "> <" + kNS + "key> " + strconv.Quote(key) + " } }"
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return false
	}
	r, err := ParseResults(body)
	return err == nil && r.Boolean != nil && *r.Boolean
}

// makeView copies the graphs of def into slot's view, writes the query projection of its claims
// (claimsNT) beside it, and marks both with key, all in one update: the store applies an update
// whole, so a reader sees the old view or the new one, never a view without its claims. Building
// takes turns per slot, and a builder that waited finds the view already made under its key. chain is
// the view's chain graph (ADR-022): the index's own bookkeeping, kept like a scope graph but never part
// of the union of the claims, whose nodes carry the same triples.
func (d *Daemon) makeView(ctx context.Context, slot, key string, def []string, claims []Quad, scopes map[string][]Quad, chain []Quad, built builtInfo) (err error) {
	if d.Dir != "" {
		if err := os.MkdirAll(d.datasetDir(), 0o755); err != nil {
			return err
		}
		unlock, err := flock.Lock(filepath.Join(d.datasetDir(), slot+".lock"))
		if err != nil {
			return err
		}
		defer unlock()
		if d.viewIs(ctx, slot, key) {
			return nil
		}
		// What a build killed before its last update staged is swept first, only under the slot lock:
		// without it two builders could sweep each other's staging (kton-review S1 and its note on #221).
		if err := d.sweepStaging(ctx, slot); err != nil {
			return err
		}
	}
	v, c, m := "<"+viewIRI(slot)+">", "<"+claimsIRI(slot)+">", "<"+markIRI(slot)+">"
	// The view is replaced graph by graph: a graph whose content did not change since the view it
	// replaces (by its hash, kept in the mark graph) is left in place, and the union of the claims is
	// changed by what left and what came. A say into one scope then rewrites that scope's graph and the
	// small rest, not the whole view (MH, 21.11.: a minute of blank after signing). A view kept from
	// before the hashes is replaced whole.
	var rest []Quad
	for _, q := range claims {
		if q.G == "" {
			rest = append(rest, q)
		}
	}
	upload := map[string][]Quad{restIRI(slot): rest, chainIRI(slot): chain}
	for g, qs := range scopes {
		upload[g] = qs
	}
	hashes := map[string]string{}
	for g, qs := range upload {
		hashes[g] = core.HashBytes(NTriples(qs))
	}
	defHash := core.HashBytes([]byte(strings.Join(def, "\n")))
	oldHashes, oldDef, incremental, err := d.viewHashes(ctx, slot)
	if err != nil {
		return err
	}
	var changed, removed []string
	for _, g := range sortedKeys(upload) {
		if !incremental || oldHashes[g] != hashes[g] {
			changed = append(changed, g)
		}
	}
	if incremental {
		for _, g := range sortedKeys(oldHashes) {
			if _, still := upload[g]; !still {
				removed = append(removed, g)
			}
		}
	}

	// The data goes up first, through the graph store, in chunks, into staging graphs no query lists:
	// one SPARQL update with every claim in it outgrew the engine's 128 MiB body limit in large federated
	// views (kton-coordination and userinterface, 21.11.). Then one small update puts the staging graphs
	// in place, so a reader still sees the old view or the new one whole.
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	staging := func(g string) string { return g + ":staging:" + hex.EncodeToString(nonce) }
	var staged []string
	defer func() {
		if err != nil {
			for _, g := range staged {
				_, _ = d.request(context.Background(), "/update", "", "update", "DROP SILENT GRAPH <"+g+">")
			}
		}
	}()
	for _, g := range changed {
		staged = append(staged, staging(g))
		if err = d.stage(ctx, staging(g), upload[g]); err != nil {
			return fmt.Errorf("staging %s: %w", g, err)
		}
	}
	var u bytes.Buffer
	if !incremental {
		// The scope graphs of the view being replaced are named in its metadata; they go with it.
		old, err := d.claimGraphs(ctx, slot)
		if err != nil {
			return err
		}
		for _, g := range old {
			u.WriteString("DROP SILENT GRAPH <" + g + "> ;\n")
		}
		u.WriteString("DROP SILENT GRAPH <" + restIRI(slot) + "> ;\nDROP SILENT GRAPH <" + chainIRI(slot) + "> ;\nDROP SILENT GRAPH " + c + " ;\n")
	} else {
		// What a changed or removed graph held leaves the union before the graph goes. The graphs hold
		// disjoint triples: a scope graph only its own claims' triples (a claim is in one scope), and what
		// claims assert about something else, which several may assert alike, is in the rest with the
		// signer labels and the metadata (kton-review N1 on #226).
		for _, g := range append(append([]string{}, removed...), changed...) {
			if _, had := oldHashes[g]; !had {
				continue
			}
			if g != chainIRI(slot) {
				u.WriteString("DELETE { GRAPH " + c + " { ?s ?p ?o } } WHERE { GRAPH <" + g + "> { ?s ?p ?o } } ;\n")
			}
			u.WriteString("DROP SILENT GRAPH <" + g + "> ;\n")
		}
	}
	if !incremental || defHash != oldDef {
		u.WriteString("DROP SILENT GRAPH " + v + " ;\n")
		if len(def) > 0 {
			u.WriteString("INSERT { GRAPH " + v + " { ?s ?p ?o } } WHERE { VALUES ?g {")
			for _, g := range def {
				u.WriteString(" <" + g + ">")
			}
			u.WriteString(" } GRAPH ?g { ?s ?p ?o } } ;\n")
		}
	}
	// MOVE SILENT from a graph that was never staged (an empty upload) leaves its target empty, which it
	// is anyway: it was dropped above. Each graph moved into place is added to the union by the engine,
	// so a claim's triples are sent once.
	for _, g := range changed {
		u.WriteString("MOVE SILENT <" + staging(g) + "> TO <" + g + "> ;\n")
		if g != chainIRI(slot) {
			u.WriteString("ADD <" + g + "> TO " + c + " ;\n")
		}
	}
	u.WriteString("DROP SILENT GRAPH " + m + " ;\nINSERT DATA { GRAPH " + m + " { " + v + " <" + kNS + "key> " + strconv.Quote(key) +
		" ; <" + kNS + "defHash> " + strconv.Quote(defHash) + " .\n")
	for _, g := range sortedKeys(hashes) {
		u.WriteString("<" + g + "> <" + kNS + "hash> " + strconv.Quote(hashes[g]) + " .\n")
	}
	u.WriteString(built.markTriples(slot))
	u.WriteString("} }")
	err = d.update(ctx, u.Bytes())
	return err
}

// viewHashes are the hashes of slot's view's graphs and of its record list, as the mark graph keeps
// them; ok is false for a view built before they were kept (or none), which is then replaced whole.
func (d *Daemon) viewHashes(ctx context.Context, slot string) (graphs map[string]string, def string, ok bool, err error) {
	q := "SELECT ?g ?h WHERE { GRAPH <" + markIRI(slot) + "> { ?g <" + kNS + "hash> ?h } }"
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return nil, "", false, err
	}
	r, err := ParseResults(body)
	if err != nil {
		return nil, "", false, err
	}
	graphs = map[string]string{}
	for _, row := range r.Rows {
		graphs[row["g"]] = row["h"]
	}
	q = "SELECT ?h WHERE { GRAPH <" + markIRI(slot) + "> { <" + viewIRI(slot) + "> <" + kNS + "defHash> ?h } }"
	if body, err = d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}}); err != nil {
		return nil, "", false, err
	}
	if r, err = ParseResults(body); err != nil {
		return nil, "", false, err
	}
	if len(r.Rows) == 0 {
		return nil, "", false, nil
	}
	return graphs, r.Rows[0]["h"], true, nil
}

// stageChunk is how much of a graph one graph-store request carries: well under the engine's body limit.
var stageChunk = 32 << 20

// stage appends qs to graph g through the graph store, in chunks of about stageChunk bytes cut only
// where the subject changes, so a node's triples (and any blank node among them) stay in one request.
// staged counts the graphs uploaded, for a test that a rebuild uploads only what changed.
var staged atomic.Int64

func (d *Daemon) stage(ctx context.Context, g string, qs []Quad) error {
	staged.Add(1)
	var b bytes.Buffer
	send := func() error {
		if b.Len() == 0 {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+"/store?graph="+url.QueryEscape(g), bytes.NewReader(b.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/n-triples")
		_, err = do(req)
		b.Reset()
		return err
	}
	for i, q := range qs {
		// Each graph-store request gets fresh blank nodes, so a blank node's link could be lost at a
		// chunk cut. The view holds none today; one that appears fails here, loudly (kton-review B1).
		if q.S.Type == "bnode" || q.O.Type == "bnode" {
			return fmt.Errorf("a blank node in the view (%s): the view is written in chunks, and a blank node cannot cross them", ntTerm(q.S))
		}
		if b.Len() >= stageChunk && i > 0 && q.S != qs[i-1].S {
			if err := send(); err != nil {
				return err
			}
		}
		b.WriteString(ntTerm(q.S) + " " + ntTerm(q.P) + " " + ntTerm(q.O) + " .\n")
	}
	return send()
}

// update sends one SPARQL update as the request body, which a large one needs: a form would
// encode every byte of it again.
func (d *Daemon) update(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+"/update", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-update")
	_, err = do(req)
	return err
}

func (d *Daemon) keepDataset(slot, key string, p *prepared) {
	if d.Dir == "" || key == "" {
		return
	}
	b, _ := json.Marshal(keptFile{key, p.named, p.scopes, p.graphs, p.registries})
	if os.MkdirAll(d.datasetDir(), 0o755) != nil {
		return
	}
	tmp := filepath.Join(d.datasetDir(), slot+"."+strconv.Itoa(os.Getpid())+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(d.datasetDir(), slot+".json"))
	}
}

// forgetDatasets drops every kept dataset: the store they describe was emptied.
func (d *Daemon) forgetDatasets() {
	if d.Dir != "" {
		os.RemoveAll(d.datasetDir())
	}
}

// claimGraphs are the scope graphs slot's view holds now, as its metadata names them.
func (d *Daemon) claimGraphs(ctx context.Context, slot string) ([]string, error) {
	q := "SELECT ?g WHERE { GRAPH <" + claimsIRI(slot) + "> { ?g a <" + nkNS + "ClaimGraph> } }"
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	r, err := ParseResults(body)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range r.Rows {
		out = append(out, row["g"])
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sweepStaging drops slot's staging graphs: what a build staged and never put in place.
func (d *Daemon) sweepStaging(ctx context.Context, slot string) error {
	q := `SELECT DISTINCT ?g WHERE { GRAPH ?g { } FILTER(STRSTARTS(STR(?g), "` + kNS + "view:" + slot + `:") && CONTAINS(STR(?g), ":staging:")) }`
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return err
	}
	r, err := ParseResults(body)
	if err != nil {
		return err
	}
	for _, row := range r.Rows {
		if _, err := d.request(ctx, "/update", "", "update", "DROP SILENT GRAPH <"+row["g"]+">"); err != nil {
			return err
		}
	}
	return nil
}
