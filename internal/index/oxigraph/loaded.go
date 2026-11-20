package oxigraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// loaded.go keeps what `cockpit load` puts into the index (ADR-017): a file's RDF as a named graph
// whose name is the file's own IRI, https://kton.dev/o/<sha256 of its bytes>, and per repository a
// catalogue of the graphs loaded for it. Nothing here is signed or recorded outside the store: a
// rebuild of the index drops it, and whoever loaded it loads it again.

const (
	fileNS = "https://kton.dev/o/"
	// loadedNS names a loaded graph's catalogue entry fields, in the catalogue graph only.
	loadedNS = kNS + "loaded#"
)

// ErrLoadRefused is a load the index declines: a limit, or content the engine cannot read.
var ErrLoadRefused = errors.New("not loaded")

// LoadedGraph is one entry of a repository's catalogue.
type LoadedGraph struct {
	Graph     string `json:"graph"`     // https://kton.dev/o/<hex>
	Hash      string `json:"hash"`      // sha256:<hex>
	From      string `json:"from"`      // the path or hash it was loaded from
	MediaType string `json:"mediaType"` // the parser it was read with
	Triples   int64  `json:"triples"`
	Loaded    string `json:"loaded"` // when, RFC 3339
}

// CatalogueIRI is the graph listing what was loaded for the repository at root.
func CatalogueIRI(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return kNS + "loaded:" + digest([]string{abs})
}

// GraphOf is the graph a file's RDF is loaded into: its own IRI.
func GraphOf(hexHash string) string { return fileNS + hexHash }

// LoadGraph puts data, read as mediaType, into the graph of hexHash, and lists it in the catalogue
// of the repository at cfg.RepoRoot, refusing a load that would take the repository past maxTriples.
// The graph is replaced whole: the same hash is the same bytes, so loading it again changes nothing.
func (d *Daemon) LoadGraph(ctx context.Context, cfg *config.Config, hexHash string, data []byte, mediaType, from string) (*LoadedGraph, error) {
	if err := d.schema(ctx, true); err != nil {
		return nil, err
	}
	g := GraphOf(hexHash)
	cat := CatalogueIRI(cfg.RepoRoot)
	shared, err := d.listedAnywhere(ctx, g)
	if err != nil {
		return nil, err
	}
	if !shared {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, d.URL+"/store?graph="+url.QueryEscape(g), bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mediaType)
		if _, err := do(req); err != nil {
			_, _ = d.request(ctx, "/update", "", "update", "DROP SILENT GRAPH <"+g+">")
			return nil, fmt.Errorf("%w: the engine could not read it as %s: %v", ErrLoadRefused, mediaType, err)
		}
	}
	n, err := d.count(ctx, `SELECT (COUNT(*) AS ?n) WHERE { GRAPH <`+g+`> { ?s ?p ?o } }`)
	if err != nil {
		return nil, err
	}
	have, err := d.loadedTriples(ctx, cat, g)
	if err != nil {
		return nil, err
	}
	if limit := cfg.Raw.Index.Derived.MaxTriplesOrDefault(); have+n > limit {
		if !shared {
			_, _ = d.request(ctx, "/update", "", "update", "DROP SILENT GRAPH <"+g+">")
		}
		return nil, fmt.Errorf("%w: %d triples, and this repository has %d loaded already: past index.derived.maxTriples (%d)", ErrLoadRefused, n, have, limit)
	}
	when := time.Now().UTC().Format(time.RFC3339)
	update := fmt.Sprintf(`DELETE WHERE { GRAPH <%[1]s> { <%[2]s> ?p ?o } } ;
INSERT DATA { GRAPH <%[1]s> { <%[2]s> <%[3]sfrom> %[4]s ; <%[3]smediaType> %[5]s ; <%[3]striples> "%[6]d"^^<http://www.w3.org/2001/XMLSchema#integer> ; <%[3]sloaded> "%[7]s"^^<http://www.w3.org/2001/XMLSchema#dateTime> } }`,
		cat, g, loadedNS, nqLit(from), nqLit(mediaType), n, when)
	if _, err := d.request(ctx, "/update", "", "update", update); err != nil {
		return nil, err
	}
	return &LoadedGraph{Graph: g, Hash: "sha256:" + hexHash, From: from, MediaType: mediaType, Triples: n, Loaded: when}, nil
}

// Loaded lists the catalogue of the repository at root, by graph.
func (d *Daemon) Loaded(ctx context.Context, root string) ([]LoadedGraph, error) {
	if err := d.schema(ctx, true); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT ?g ?from ?type ?n ?when WHERE { GRAPH <%s> { ?g <%[2]sfrom> ?from ; <%[2]smediaType> ?type ; <%[2]striples> ?n ; <%[2]sloaded> ?when } } ORDER BY ?g`,
		CatalogueIRI(root), loadedNS)
	rows, err := d.selectCSV(ctx, url.Values{"query": {q}}) // the store's own dataset: the catalogue is a named graph
	if err != nil {
		return nil, err
	}
	out := []LoadedGraph{}
	for _, r := range rows {
		n, _ := strconv.ParseInt(r["n"], 10, 64)
		out = append(out, LoadedGraph{Graph: r["g"], Hash: "sha256:" + strings.TrimPrefix(r["g"], fileNS),
			From: r["from"], MediaType: r["type"], Triples: n, Loaded: r["when"]})
	}
	return out, nil
}

// LoadedGraphs is the graph names a query of the repository at root sees as its named graphs.
func (d *Daemon) LoadedGraphs(ctx context.Context, root string) ([]string, error) {
	l, err := d.Loaded(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(l))
	for i, g := range l {
		out[i] = g.Graph
	}
	sort.Strings(out)
	return out, nil
}

// DropLoaded takes the graph of hexHash out of the repository's catalogue, and out of the store when
// no other repository's catalogue lists it. It says whether the repository had it.
func (d *Daemon) DropLoaded(ctx context.Context, root, hexHash string) (bool, error) {
	if err := d.schema(ctx, true); err != nil {
		return false, err
	}
	g, cat := GraphOf(hexHash), CatalogueIRI(root)
	had, err := d.count(ctx, `SELECT (COUNT(*) AS ?n) WHERE { GRAPH <`+cat+`> { <`+g+`> ?p ?o } }`)
	if err != nil || had == 0 {
		return false, err
	}
	if _, err := d.request(ctx, "/update", "", "update", `DELETE WHERE { GRAPH <`+cat+`> { <`+g+`> ?p ?o } }`); err != nil {
		return true, err
	}
	still, err := d.listedAnywhere(ctx, g)
	if err != nil {
		return true, err
	}
	if !still {
		_, err = d.request(ctx, "/update", "", "update", "DROP SILENT GRAPH <"+g+">")
	}
	return true, err
}

// listedAnywhere says whether any repository's catalogue lists graph g: the graph is then in the store
// and in use, and its bytes are the same whoever loaded them.
func (d *Daemon) listedAnywhere(ctx context.Context, g string) (bool, error) {
	n, err := d.count(ctx, fmt.Sprintf(`SELECT (COUNT(*) AS ?n) WHERE { GRAPH ?c { <%s> <%sfrom> ?f } FILTER(STRSTARTS(STR(?c), "%sloaded:")) }`, g, loadedNS, kNS))
	return n > 0, err
}

// loadedTriples is how many triples the catalogue cat lists, without graph g.
func (d *Daemon) loadedTriples(ctx context.Context, cat, g string) (int64, error) {
	return d.count(ctx, fmt.Sprintf(`SELECT (SUM(?n) AS ?total) WHERE { GRAPH <%s> { ?g <%striples> ?n } FILTER(?g != <%s>) }`, cat, loadedNS, g))
}

// count runs a query whose one row has one number, and reads it (0 for an empty answer).
func (d *Daemon) count(ctx context.Context, q string) (int64, error) {
	body, err := d.request(ctx, "/query", "text/csv", "query", q)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		return 0, nil
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	return n, nil
}
