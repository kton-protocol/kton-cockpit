package cockpit

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nclaim "kton.dev/nekton/claim"
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
	Version     int                 `json:"version"`
	Dataset     string              `json:"dataset"`
	Bindings    map[string]string   `json:"bindings,omitempty"`
	Registries  []PinnedRegistry    `json:"registries"`
	Scopes      map[string][]string `json:"scopes"` // a scope's seed -> the tips of its chains the run read
	Trust       []PinnedKey         `json:"trust"`
	Templates   string              `json:"templates"`          // sha256 of the template set, canonical, in the content store
	Aliases     string              `json:"aliases,omitempty"`  // sha256 of .kton/aliases.json
	Packages    []string            `json:"packages,omitempty"` // admitted package revisions: <package id>@<revision id>
	ViewVersion int                 `json:"viewVersion"`
	Engine      string              `json:"engine"`
}

// PinnedRegistry is one registry the run read: the sorted ids of its records and claims, each list in
// the content store.
type PinnedRegistry struct {
	Name    string `json:"name"`
	Records string `json:"records"`
	Claims  string `json:"claims"`
}

// PinnedKey is a key the run's trust counted, with its tier.
type PinnedKey struct {
	Key  string `json:"key"`
	Tier string `json:"tier"`
}

// queryRun evaluates q as a query run and records it.
func queryRun(ctx context.Context, cfg *config.Config, q, dataset string, bindings map[string]string, timeout time.Duration) (*SparqlAnswer, *QueryRun, error) {
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
	keys, err := trustedKeys(cfg)
	if err != nil {
		return nil, nil, err
	}
	body, _, err := inContainer(ctx, cfg, keys, []byte(q), "json", dataset, timeout)
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
	manifest, blobs, err := pinDataset(ctx, cfg, dataset, bindings)
	if err != nil {
		return nil, nil, refuse("io", "ADR-020", "pinning what the query read: %v", err)
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")

	store, err := blobstore.Open(cfg.BlobsDir)
	if err != nil {
		return nil, nil, refuse("io", "", "%v", err)
	}
	put := func(b []byte) (string, error) { return store.Put(b) }
	for _, b := range blobs {
		if _, err := put(b); err != nil {
			return nil, nil, refuse("io", "", "%v", err)
		}
	}
	run := &QueryRun{}
	stage, err := os.MkdirTemp(filepath.Join(cfg.LocalDir), "queryrun-")
	if err != nil {
		return nil, nil, refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(stage)
	for _, f := range []struct {
		name string
		b    []byte
		into *string
	}{{"query.rq", []byte(q), &run.Query}, {"manifest.json", mb, &run.Manifest}, {"result.json", result, &run.Result}} {
		h, err := put(f.b)
		if err != nil {
			return nil, nil, refuse("io", "", "%v", err)
		}
		*f.into = h
		if err := os.WriteFile(filepath.Join(stage, f.name), f.b, 0o600); err != nil {
			return nil, nil, refuse("io", "", "%v", err)
		}
	}
	a, err := binaries.New(cfg).Author(ctx, binaries.AuthorInput{
		Dir: stage, Inputs: []string{"query.rq", "manifest.json"}, Outputs: []string{"result.json"},
		SignKey: cfg.PlanktonKey, Kind: "query/sparql",
		Descriptor: map[string]any{"dataset": dataset, "viewVersion": oxigraph.ViewVersion(), "engine": cfg.Raw.Query.Image},
	})
	if err != nil {
		return nil, nil, refuse("kernel", "", "recording the query run: %v", err)
	}
	run.FotonID = a.ID
	rev, err := persist(ctx, cfg, []string{config.Rel("plankton"), config.Rel("blobs")}, "query run: "+a.ID)
	if err != nil {
		return nil, nil, refuse(storeCode(err), "", "persisting the query run: %v", err)
	}
	run.Revision = rev.ID
	return answerOf(form, res, "a container for this query run"), run, nil
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

// pinDataset is the manifest of what a query over cfg's union reads now, and the blobs it names.
func pinDataset(ctx context.Context, cfg *config.Config, dataset string, bindings map[string]string) (*QueryManifest, [][]byte, error) {
	m := &QueryManifest{Version: 1, Dataset: dataset, Bindings: bindings, Scopes: map[string][]string{},
		ViewVersion: oxigraph.ViewVersion(), Engine: cfg.Raw.Query.Image}
	var blobs [][]byte
	keep := func(b []byte) string {
		blobs = append(blobs, b)
		return core.HashBytes(b)
	}
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
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
				claims = append(claims, r.ClaimID)
				st, _, err := nclaim.ParseEnvelope(r.Envelope)
				if err != nil {
					continue
				}
				var p nclaim.Predicate
				// A seed names its scope by name in the same field; it opens the chain, and is no member of it.
				if json.Unmarshal(st.Predicate, &p) == nil && p.Scope != "" && !p.Genesis {
					scopeOf[r.ClaimID] = p.Scope
					if p.Prev != "" {
						prev[p.Prev] = true
					}
				}
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
			return nil, nil, err
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, nil, err
		}
		m.Trust = append(m.Trust, PinnedKey{Key: hex.EncodeToString(pub), Tier: tier})
	}
	sort.Slice(m.Trust, func(i, j int) bool { return m.Trust[i].Key < m.Trust[j].Key })
	set, err := binaries.New(cfg).Templates()
	if err != nil {
		return nil, nil, err
	}
	tpl := map[string]any{}
	for _, n := range set.Names() {
		t, _ := set.Get(n)
		tpl[n] = t
	}
	tb, _ := json.Marshal(tpl)
	m.Templates = keep(tb)
	if b, err := os.ReadFile(filepath.Join(cfg.KtonDir, "aliases.json")); err == nil {
		m.Aliases = keep(b)
	}
	if usable, _, err := packages.Usable(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages); err == nil {
		for _, in := range usable {
			m.Packages = append(m.Packages, in.Package.ID+"@"+in.Revision.ID)
		}
		sort.Strings(m.Packages)
	}
	return m, blobs, nil
}
