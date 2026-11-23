package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nclaim "kton.dev/nekton/claim"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// A query run (ADR-020): a query evaluated over its pinned dataset, built fresh in a container of its
// own (never the live index, so the first run and every reproduction take one path), recorded as a foton
// whose inputs are the query and the manifest of everything it read and whose output is its canonical
// result. The bytes go into the repository's content store, so anyone can reproduce it from the records.

// afterPin runs between pinning and evaluating, for a test: whatever it signs must not be in the run.
var afterPin func()

// QueryRun is what a query run recorded.
type QueryRun struct {
	FotonID  string `json:"fotonId"`
	Manifest string `json:"manifest"` // sha256 of the manifest in the content store
	Query    string `json:"query"`    // sha256 of the query text
	Result   string `json:"result"`   // sha256 of the canonical result
	Revision string `json:"revision,omitempty"`
}

// QueryManifest is everything a query run's rows depend on, pinned (ADR-020, section 2).
type QueryManifest struct {
	Version    int                 `json:"version"`
	Dataset    string              `json:"dataset"`
	Bindings   map[string]string   `json:"bindings,omitempty"`
	Registries []PinnedRegistry    `json:"registries"`
	Scopes     map[string][]string `json:"scopes"` // a scope's seed -> the tips of its chains the run read
	Trust      []PinnedKey         `json:"trust"`
	View       string              `json:"view"`               // sha256 of the pinned view (pinnedView), in the content store
	Packages   []string            `json:"packages,omitempty"` // admitted package revisions: <package id>@<revision id>
	// Loaded are the files loaded for this repository that the query read as graphs (ADR-017), each by its
	// hash, its bytes in the content store; only in the claims dataset with index.derived on, as the index.
	// Absent: none.
	Loaded []PinnedLoad `json:"loaded,omitempty"`
	// Graphs are the scope seeds and loaded files the run was narrowed to (--graph), resolved; absent: every
	// one. Unscoped claims and the steps are inputs either way.
	Graphs      []string `json:"graphs,omitempty"`
	ViewVersion int      `json:"viewVersion"`
	Engine      string   `json:"engine"`
}

// manifestVersion is the manifest this cockpit writes and reproduces. Version 1 (v0.5.10) pinned the
// templates but not the signers' names, the row templates or the packages' aliases, so its view cannot
// be rebuilt exactly; it is refused, named.
const manifestVersion = 2

// PinnedRegistry is one registry the run read: the sorted ids of its records and claims, each list in
// the content store.
type PinnedRegistry struct {
	Name    string `json:"name"`
	Records string `json:"records"`
	Claims  string `json:"claims"`
}

// PinnedLoad is one loaded file the run read: its hash and the media type it was read as.
type PinnedLoad struct {
	Hash      string `json:"hash"`
	MediaType string `json:"mediaType"`
}

// loadedBytes is a pinned loaded file with its bytes, for the evaluation.
type loadedBytes struct {
	hex, mediaType string
	data           []byte
}

// PinnedKey is a key the run's trust counted, with its tier.
type PinnedKey struct {
	Key  string `json:"key"`
	Tier string `json:"tier"`
}

// pinnedView is the view a query run evaluated with (ADR-020: the trust keys that counted, the
// templates and alias files, the row templates), as bytes, so a reproduction rebuilds it exactly.
type pinnedView struct {
	Signers        map[string]oxigraph.Signer  `json:"signers"`           // by keyid
	Templates      map[string]json.RawMessage  `json:"templates"`         // by name, as the set holds them
	Aliases        json.RawMessage             `json:"aliases,omitempty"` // the repository's alias file
	PackageAliases map[string]packages.Aliases `json:"packageAliases,omitempty"`
	Rows           []string                    `json:"rows,omitempty"`
}

// currentView is cfg's view as a query run pins it.
func currentView(cfg *config.Config) (*pinnedView, error) {
	vc, err := oxigraph.ViewConfigFor(cfg)
	if err != nil {
		return nil, err
	}
	pv := &pinnedView{Signers: vc.Signers, Templates: map[string]json.RawMessage{}, PackageAliases: vc.Aliases}
	for _, n := range vc.Templates.Names() {
		t, _ := vc.Templates.Get(n)
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		pv.Templates[n] = b
	}
	if b, err := os.ReadFile(filepath.Join(cfg.KtonDir, "aliases.json")); err == nil {
		pv.Aliases = b
	}
	for n := range vc.Rows {
		pv.Rows = append(pv.Rows, n)
	}
	sort.Strings(pv.Rows)
	return pv, nil
}

// config is the view pv pins, as the projection takes it.
func (pv *pinnedView) config() (oxigraph.ViewConfig, error) {
	raw := map[string][]byte{}
	for n, b := range pv.Templates {
		raw[n] = b
	}
	set, err := ntemplate.New(raw, pv.Aliases)
	if err != nil {
		return oxigraph.ViewConfig{}, err
	}
	vc := oxigraph.ViewConfig{Signers: pv.Signers, Templates: set, Aliases: pv.PackageAliases, Rows: map[string]bool{}}
	if vc.Signers == nil {
		vc.Signers = map[string]oxigraph.Signer{}
	}
	for _, n := range pv.Rows {
		vc.Rows[n] = true
	}
	return vc, nil
}

// trustKeys are the keys a manifest pins.
func (m *QueryManifest) trustKeys() ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for _, k := range m.Trust {
		raw, err := hex.DecodeString(k.Key)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("the manifest pins %q, which is not a public key", k.Key)
		}
		keys = append(keys, ed25519.PublicKey(raw))
	}
	return keys, nil
}

// queryRun evaluates q as a query run and records it.
func queryRun(ctx context.Context, cfg *config.Config, q, dataset string, bindings map[string]string, graphs []string, timeout time.Duration) (*SparqlAnswer, *QueryRun, error) {
	form, err := index.CheckFreeQuery(q)
	if err != nil {
		return nil, nil, refuse("query.invalid", "SPEC §9.9", "%v", err)
	}
	if err := checkReproducible(q); err != nil {
		return nil, nil, refuse("query.not-reproducible", "ADR-020", "%v", err)
	}
	if cfg.Raw.Query.Image == "" {
		return nil, nil, refuse("query.not-configured", "ADR-020", "a query run is evaluated in a container of its own: set query.image")
	}
	// Pinned first, then evaluated over exactly the pins: a claim signed meanwhile, or a source that moves,
	// is in neither the rows nor the manifest (kton-review Q1 on #225; ADR-020 A2).
	manifest, blobs, pinned, err := pinDataset(ctx, cfg, dataset, bindings, graphs)
	var ref *Refusal
	if errors.As(err, &ref) {
		return nil, nil, err
	}
	if err != nil {
		return nil, nil, refuse("io", "ADR-020", "pinning what the query reads: %v", err)
	}
	if afterPin != nil {
		afterPin()
	}
	res, result, err := evaluatePinned(ctx, cfg, q, manifest, pinned, timeout)
	if err != nil {
		return nil, nil, err
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	store, err := blobstore.Open(cfg.BlobsDir)
	if err != nil {
		return nil, nil, refuse("io", "", "%v", err)
	}
	for _, b := range blobs {
		if _, err := store.Put(b); err != nil {
			return nil, nil, refuse("io", "", "%v", err)
		}
	}
	run, err := recordQueryRun(ctx, cfg, store, []byte(q), mb, result,
		map[string]any{"dataset": dataset, "viewVersion": oxigraph.ViewVersion(), "engine": cfg.Raw.Query.Image}, "query run")
	if err != nil {
		return nil, nil, err
	}
	ans := answerOf(form, res, "a container for this query run")
	ans.LoadedGraphs = len(manifest.Loaded)
	return ans, run, nil
}

// evaluatePinned evaluates q over exactly what m and pinned name, with m's engine, trust and view: the
// one path a query run and its reproductions take. It answers the engine's results and their
// canonical form.
func evaluatePinned(ctx context.Context, cfg *config.Config, q string, m *QueryManifest, pinned *pinnedSet, timeout time.Duration) (*oxigraph.Results, []byte, error) {
	keys, err := m.trustKeys()
	if err != nil {
		return nil, nil, refuse("query.not-reproducible", "ADR-020", "%v", err)
	}
	vc, err := pinned.view.config()
	if err != nil {
		return nil, nil, refuse("query.not-reproducible", "ADR-020", "the pinned view cannot be rebuilt: %v", err)
	}
	tmp := filepath.Join(cfg.LocalDir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, nil, refuse("io", "", "%v", err)
	}
	built, err := os.MkdirTemp(tmp, "pinned-")
	if err != nil {
		return nil, nil, refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(built)
	found := map[string]bool{}
	u, err := binaries.PinnedUnion(built, pinned.inputs, func(id string) bool {
		if pinned.ids[id] {
			found[id] = true
			return true
		}
		return false
	})
	if err != nil {
		return nil, nil, refuse("kernel", "ADR-020", "building the pinned dataset: %v", err)
	}
	// Every pinned id must be in what is evaluated: a source whose history was rewritten between pinning
	// and building would otherwise give a run over less than its manifest names (kton-review on #225).
	if len(found) != len(pinned.ids) {
		var missing []string
		for id := range pinned.ids {
			if !found[id] {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return nil, nil, refuse("query.not-reproducible", "ADR-020", "%d of the %d records and claims pinned are in no registry this repository reads, the first %s", len(missing), len(pinned.ids), missing[0])
	}
	body, _, err := inContainerOver(ctx, cfg, keys, u, &evalPin{image: m.Engine, vc: vc, sources: pinned.sources, loaded: pinned.loaded}, []byte(q), "json", m.Dataset, timeout)
	if err != nil {
		return nil, nil, err
	}
	res, err := oxigraph.ParseResults([]byte(body))
	if err != nil {
		return nil, nil, refuse("query.failed", "", "%v", err)
	}
	result, err := canonicalResult(res)
	if err != nil {
		return nil, nil, refuse("query.not-reproducible", "ADR-020", "%v", err)
	}
	return res, result, nil
}

// recordQueryRun signs a query/sparql foton over the query, the manifest and the result, with every
// byte in the content store, and persists it. A reproduction records the same foton again, signed by
// its reproducer.
func recordQueryRun(ctx context.Context, cfg *config.Config, store *blobstore.Store, q, manifest, result []byte, descriptor map[string]any, what string) (*QueryRun, error) {
	run := &QueryRun{}
	stage, err := os.MkdirTemp(filepath.Join(cfg.LocalDir), "queryrun-")
	if err != nil {
		return nil, refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(stage)
	for _, f := range []struct {
		name string
		b    []byte
		into *string
	}{{"query.rq", q, &run.Query}, {"manifest.json", manifest, &run.Manifest}, {"result.json", result, &run.Result}} {
		h, err := store.Put(f.b)
		if err != nil {
			return nil, refuse("io", "", "%v", err)
		}
		*f.into = h
		if err := os.WriteFile(filepath.Join(stage, f.name), f.b, 0o600); err != nil {
			return nil, refuse("io", "", "%v", err)
		}
	}
	a, err := binaries.New(cfg).Author(ctx, binaries.AuthorInput{
		Dir: stage, Inputs: []string{"query.rq", "manifest.json"}, Outputs: []string{"result.json"},
		SignKey: cfg.PlanktonKey, Kind: "query/sparql", Descriptor: descriptor,
	})
	if err != nil {
		return nil, refuse("kernel", "", "recording the %s: %v", what, err)
	}
	run.FotonID = a.ID
	rev, err := persist(ctx, cfg, []string{config.Rel("plankton"), config.Rel("blobs")}, what+": "+a.ID)
	if err != nil {
		return nil, refuse(storeCode(err), "", "persisting the %s: %v", what, err)
	}
	run.Revision = rev.ID
	return run, nil
}

// canonicalResult is a SELECT's rows as a sorted multiset of RDF terms in N-Triples form, or an ASK's
// boolean: the same answer gives the same bytes whatever order the engine returned it in. A blank node
// is refused: no canonical label for it (kton-review A1-A4 on ADR-020).
func canonicalResult(r *oxigraph.Results) ([]byte, error) {
	if r.Boolean != nil {
		return json.Marshal(map[string]any{"boolean": *r.Boolean})
	}
	rows := make([]string, 0, len(r.Terms))
	for _, t := range r.Terms {
		var cells []string
		for _, v := range r.Vars {
			term, ok := t[v]
			if !ok {
				cells = append(cells, "")
				continue
			}
			if term.Type == "bnode" {
				return nil, fmt.Errorf("the answer holds a blank node (?%s), which has no label that stays the same", v)
			}
			cells = append(cells, oxigraph.NTerm(term))
		}
		rows = append(rows, strings.Join(cells, "\t"))
	}
	sort.Strings(rows)
	return json.MarshalIndent(map[string]any{"vars": r.Vars, "rows": rows}, "", " ")
}

// pinnedSet is what pinDataset pinned: the inputs it read, and the ids of every record and claim.
type pinnedSet struct {
	inputs  []binaries.Input
	ids     map[string]bool
	sources map[string][]string // a claim's hash -> the registries that hold it, in the union's order
	view    *pinnedView
	loaded  []loadedBytes
}

// pinDataset is the manifest of what a query over cfg's union reads now, the blobs it names, and the
// ids it pinned. A scope's tips are taken over every claim on disk, which can only widen the pin, never
// narrow it (kton-review N4 on #225).
func pinDataset(ctx context.Context, cfg *config.Config, dataset string, bindings map[string]string, graphs []string) (*QueryManifest, [][]byte, *pinnedSet, error) {
	m := &QueryManifest{Version: manifestVersion, Dataset: dataset, Bindings: bindings, Scopes: map[string][]string{},
		ViewVersion: oxigraph.ViewVersion(), Engine: cfg.Raw.Query.Image}
	var blobs [][]byte
	keep := func(b []byte) string {
		blobs = append(blobs, b)
		return core.HashBytes(b)
	}
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	pinned := &pinnedSet{inputs: inputs, ids: map[string]bool{}, sources: map[string][]string{}}
	withLoads := dataset == oxigraph.DatasetClaims && cfg.Raw.Index.Derived.Enabled
	var list []loadEntry
	if withLoads {
		if list, err = readLoadList(cfg); err != nil {
			return nil, nil, nil, err
		}
	}
	scopes, loads, err := narrowTo(cfg, graphs, list)
	if err != nil {
		return nil, nil, nil, err
	}
	seeds := map[string]bool{}
	prev := map[string]bool{}
	scopeOf := map[string]string{}
	for _, in := range inputs {
		name := in.Name
		if name == binaries.Own {
			name = "own"
		}
		var recs, claims []string
		if p, err := binaries.OpenPlanktonSource(in.PlanktonDir); err == nil {
			for _, r := range p.Records(0) {
				recs = append(recs, r.FotonID)
			}
		}
		if n, err := binaries.OpenNektonSource(in.NektonDir); err == nil {
			for _, r := range n.RawRecords() {
				st, _, err := nclaim.ParseEnvelope(r.Envelope)
				var p nclaim.Predicate
				parsed := err == nil && json.Unmarshal(st.Predicate, &p) == nil
				// The scope a claim is in: a seed's own id, a member's scope; "" for an unscoped claim.
				in := ""
				if parsed && p.Genesis {
					in = strings.ToLower(r.ClaimID)
					seeds[in] = true
				} else if parsed && p.Scope != "" {
					in = strings.ToLower(p.Scope)
				}
				if scopes != nil && in != "" && !scopes[in] {
					continue // narrowed out (--graph)
				}
				claims = append(claims, r.ClaimID)
				// A seed names its scope by name in the same field; it opens the chain, and is no member of it.
				if parsed && p.Scope != "" && !p.Genesis {
					scopeOf[r.ClaimID] = p.Scope
					if p.Prev != "" {
						prev[p.Prev] = true
					}
				}
			}
		}
		for _, id := range recs {
			pinned.ids[id] = true
		}
		for _, id := range claims {
			pinned.ids[id] = true
			h := strings.ToLower(strings.TrimPrefix(id, "sha256:"))
			if l := pinned.sources[h]; len(l) == 0 || l[len(l)-1] != name {
				pinned.sources[h] = append(l, name)
			}
		}
		sort.Strings(recs)
		sort.Strings(claims)
		m.Registries = append(m.Registries, PinnedRegistry{Name: name,
			Records: keep([]byte(strings.Join(recs, "\n"))), Claims: keep([]byte(strings.Join(claims, "\n")))})
	}
	// A scope's tips: its claims no later claim of it names as prev (a fork has more than one).
	for id, seed := range scopeOf {
		if !prev[id] {
			m.Scopes[seed] = append(m.Scopes[seed], id)
		}
	}
	for s := range m.Scopes {
		sort.Strings(m.Scopes[s])
	}
	for path, tier := range cfg.TierPubkeys() {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, err
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, nil, nil, err
		}
		m.Trust = append(m.Trust, PinnedKey{Key: hex.EncodeToString(pub), Tier: tier})
	}
	sort.Slice(m.Trust, func(i, j int) bool { return m.Trust[i].Key < m.Trust[j].Key })
	if pinned.view, err = currentView(cfg); err != nil {
		return nil, nil, nil, err
	}
	vb, _ := json.Marshal(pinned.view)
	m.View = keep(vb)
	// The files loaded for this repository, as a query of the index reads them: in the claims dataset, with
	// index.derived on (ADR-017). A file whose bytes are no longer those it was loaded as is refused, named:
	// the run would otherwise read what no store holds.
	for s := range scopes {
		m.Graphs = append(m.Graphs, s)
		if !seeds[s] {
			return nil, nil, nil, refuse("argument", "ADR-020", "graph %s is the seed of no scope this repository reads", s)
		}
	}
	if withLoads {
		for _, e := range list {
			if loads != nil && !loads[e.Hash] {
				continue // narrowed out (--graph)
			}
			b, hex, err := loadBytes(cfg, e.From)
			if err != nil || "sha256:"+hex != e.Hash {
				return nil, nil, nil, fmt.Errorf("the loaded graph %s (from %s) no longer has its bytes there: load it again or drop it (cockpit load --drop %s)", e.Hash, e.From, e.Hash)
			}
			keep(b)
			m.Loaded = append(m.Loaded, PinnedLoad{Hash: e.Hash, MediaType: e.MediaType})
			pinned.loaded = append(pinned.loaded, loadedBytes{hex: hex, mediaType: e.MediaType, data: b})
		}
		sort.Slice(m.Loaded, func(i, j int) bool { return m.Loaded[i].Hash < m.Loaded[j].Hash })
	}
	for h := range loads {
		m.Graphs = append(m.Graphs, h)
	}
	sort.Strings(m.Graphs)
	if usable, _, err := packages.Usable(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages); err == nil {
		for _, in := range usable {
			m.Packages = append(m.Packages, in.Package.ID+"@"+in.Revision.ID)
		}
		sort.Strings(m.Packages)
	}
	return m, blobs, pinned, nil
}

// narrowTo resolves the graphs a query run is narrowed to (--graph): a scope by its configured name or its
// seed, a loaded file by its hash. Both maps are nil when graphs is empty: nothing is narrowed. A name that
// is neither is refused.
func narrowTo(cfg *config.Config, graphs []string, list []loadEntry) (scopes, loads map[string]bool, err error) {
	if len(graphs) == 0 {
		return nil, nil, nil
	}
	scopes, loads = map[string]bool{}, map[string]bool{}
	loaded := map[string]bool{}
	for _, e := range list {
		loaded[e.Hash] = true
	}
	for _, g := range graphs {
		g = strings.TrimSpace(g)
		if id, _, ok := cfg.ScopeID(g); ok {
			g = id
		}
		h, ok := core.NormalizeContentHash(g)
		if !ok || !strings.HasPrefix(strings.ToLower(g), "sha256:") {
			return nil, nil, refuse("argument", "ADR-020", "graph %q is no scope this repository configures by name, and no seed or loaded file (sha256:<hex>)", g)
		}
		if loaded[h] {
			loads[h] = true
		} else {
			scopes[h] = true
		}
	}
	return scopes, loads, nil
}
