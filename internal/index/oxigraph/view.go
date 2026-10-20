//go:build unreleased

package oxigraph

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	nclaim "kton.dev/nekton/claim"
	"kton.dev/nekton/nanopub"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/core"
	"kton.dev/plankton/rdf"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// A view is the projection of one repository's union under one set of trusted keys: exactly what a
// query answered over before the daemon existed. The kernel's projections decide attribution by
// the trusted keys (prov:wasAttributedTo only for a key that verifies the record), so a projection
// made for one key set is not the answer for another, and each set gets its own view. Trust is
// never stored as a tier: a view is named by the keys themselves, and a configuration that trusts
// other keys reads another view.
//
// Inside the one store, a view's graphs share a prefix, urn:kton:view:<id>:
//   - …:plankton holds the plankton projection (the default graph of a query);
//   - …:np:<claim id>.#Head, #assertion, … are the nanopublications, rendered with that base so two
//     views of the same claim never write into the same graph (the named graphs of a query).
//
// A query runs with exactly these graphs as its dataset, so it cannot see another view. Results
// are mapped back to the kernel's own IRIs, so an answer reads as the projection's would.
//
// The graph urn:kton:index records each view's stamp (index.Stamp), the registries it was projected
// from, and its named graphs. A query whose stamp differs projects the view again first.

const (
	indexGraph = "urn:kton:index"
	stampPred  = "urn:kton:stamp"
	graphPred  = "urn:kton:graph"
	npBase     = "https://kton.dev/np/"
)

// Answer is a query's rows and where they came from.
type Answer struct {
	Rows []map[string]string
	View string // the view id
	Kept bool   // the view was current; nothing was projected for this query
}

// Query answers a SPARQL SELECT over cfg's view, projecting it first when the registries changed.
func Query(ctx context.Context, d *Daemon, cfg *config.Config, keys []ed25519.PublicKey, query string) (*Answer, error) {
	id := viewID(cfg, keys)
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, err
	}
	stamp, err := index.Stamp(inputs)
	if err != nil {
		return nil, err
	}
	kept, err := d.current(ctx, id, stamp)
	if err != nil {
		return nil, err
	}
	if !kept {
		if err := d.project(ctx, cfg, keys, id, stamp); err != nil {
			return nil, err
		}
	}
	rows, err := d.selectOn(ctx, id, query)
	if err != nil {
		return nil, err
	}
	return &Answer{Rows: rows, View: id, Kept: kept}, nil
}

// viewID names a repository's union under a key set.
func viewID(cfg *config.Config, keys []ed25519.PublicKey) string {
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, core.KeyIDHex(k))
	}
	sort.Strings(ids)
	srcs, _ := json.Marshal(cfg.Raw.Federation.Sources)
	h := sha256.Sum256([]byte(cfg.RepoRoot + "\x00" + string(srcs) + "\x00" + strings.Join(ids, ",")))
	return hex.EncodeToString(h[:12])
}

func viewIRI(id string) string    { return "urn:kton:view:" + id }
func prefixOf(id string) string   { return viewIRI(id) + ":" }
func planktonOf(id string) string { return prefixOf(id) + "plankton" }
func npOf(id string) string       { return prefixOf(id) + "np:" }

func (d *Daemon) current(ctx context.Context, id, stamp string) (bool, error) {
	q := fmt.Sprintf(`ASK { GRAPH <%s> { <%s> <%s> %q } }`, indexGraph, viewIRI(id), stampPred, stamp)
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return false, err
	}
	var r struct {
		Boolean bool `json:"boolean"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return false, fmt.Errorf("reading the index's answer: %w", err)
	}
	return r.Boolean, nil
}

// project replaces a view: its stamp first goes (a reader meanwhile sees it as stale, never as
// current), then its graphs, then the projection is loaded and the new stamp written. Two cockpits
// projecting the same view take turns, and the second finds it current.
func (d *Daemon) project(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, id, stamp string) error {
	if d.Dir != "" {
		unlock, err := flock(filepath.Join(d.Dir, "view-"+id+".lock"))
		if err != nil {
			return err
		}
		defer unlock()
		if ok, err := d.current(ctx, id, stamp); err != nil || ok {
			return err
		}
	}
	drop := fmt.Sprintf(`DELETE WHERE { GRAPH <%s> { <%s> ?p ?o } } ;
DELETE { GRAPH ?g { ?s ?p ?o } } WHERE { GRAPH ?g { ?s ?p ?o } FILTER(STRSTARTS(STR(?g), %q)) }`,
		indexGraph, viewIRI(id), prefixOf(id))
	if _, err := d.post(ctx, "/update", "", url.Values{"update": {drop}}); err != nil {
		return fmt.Errorf("dropping view %s: %w", id, err)
	}

	u, err := binaries.OpenUnion(ctx, cfg)
	if err != nil {
		return err
	}
	ttl, err := rdf.Turtle(u.Plankton, "", keys)
	if err != nil {
		return err
	}
	if err := d.load(ctx, "text/turtle", "?graph="+url.QueryEscape(planktonOf(id)), ttl); err != nil {
		return fmt.Errorf("loading the plankton projection: %w", err)
	}
	nctx := nanopub.NewContext(ntemplate.Set{}, keys)
	var b strings.Builder
	for _, rec := range u.Nekton.Records(0) {
		// Only what a trusted key signed is projected, as before the daemon: a query answers over
		// the records ask would include, never over what merely arrived.
		if core.VerifiedSignerKeyID(rec.Envelope, keys) == "" {
			continue
		}
		st, payload, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil {
			continue
		}
		var body map[string]any
		if json.Unmarshal(st.Predicate, &body) != nil {
			continue
		}
		cid := strings.TrimPrefix(nclaim.ClaimID(payload), "sha256:")
		b.WriteString(nanopub.RenderTrig(nctx, st, body, rec.Envelope, cid, "", npOf(id)+cid+".", ""))
		b.WriteString("\n")
	}
	if b.Len() > 0 {
		if err := d.load(ctx, "application/trig", "", b.String()); err != nil {
			return fmt.Errorf("loading the nekton projection: %w", err)
		}
	}
	// The view's named graphs are listed once here, so a query does not scan the store for them.
	list := fmt.Sprintf(`INSERT { GRAPH <%s> { <%s> <%s> ?g } } WHERE { SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } FILTER(STRSTARTS(STR(?g), %q)) } }`,
		indexGraph, viewIRI(id), graphPred, npOf(id))
	if _, err := d.post(ctx, "/update", "", url.Values{"update": {list}}); err != nil {
		return fmt.Errorf("listing view %s: %w", id, err)
	}
	mark := fmt.Sprintf(`INSERT DATA { GRAPH <%s> { <%s> <%s> %q } }`, indexGraph, viewIRI(id), stampPred, stamp)
	if _, err := d.post(ctx, "/update", "", url.Values{"update": {mark}}); err != nil {
		return fmt.Errorf("stamping view %s: %w", id, err)
	}
	return nil
}

// selectOn runs query with the view as its dataset: the plankton graph as the default graph, the
// view's nanopublications as the named graphs.
func (d *Daemon) selectOn(ctx context.Context, id, query string) ([]map[string]string, error) {
	list := fmt.Sprintf(`SELECT ?g WHERE { GRAPH <%s> { <%s> <%s> ?g } }`, indexGraph, viewIRI(id), graphPred)
	body, err := d.post(ctx, "/query", "text/csv", url.Values{"query": {list}})
	if err != nil {
		return nil, err
	}
	graphs, err := readCSV(body)
	if err != nil {
		return nil, err
	}
	form := url.Values{"query": {query}, "default-graph-uri": {planktonOf(id)}}
	for _, g := range graphs {
		form.Add("named-graph-uri", g["g"])
	}
	if len(graphs) == 0 {
		// An empty named-graph list would mean "the store's own", not "none": name one nobody uses.
		form.Add("named-graph-uri", prefixOf(id)+"none")
	}
	body, err = d.post(ctx, "/query", "text/csv", form)
	if err != nil {
		return nil, err
	}
	rows, err := readCSV(body)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		for k, v := range row {
			row[k] = strings.Replace(v, npOf(id), npBase, 1)
		}
	}
	return rows, nil
}

func readCSV(b []byte) ([]map[string]string, error) {
	rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
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

func (d *Daemon) load(ctx context.Context, mediaType, query, data string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+"/store"+query, strings.NewReader(data))
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
