//go:build docker

package cockpit

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/container"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

const oxigraphImage = "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"

// The index daemon answers a query exactly as a container made for that one query does: the same
// rows, the claims' nanopublication graphs and their attribution included.
func TestQuery_TheIndexAnswersAsAContainerForTheQueryDoes(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	if res, _, err := Say(ctx, nil, SayRequest{Subject: pub.FotonID, Template: "working-on",
		Fields: map[string]string{"step": "checking", "by-session": testrepo.SessionID}}); err != nil || res.IsError {
		t.Fatalf("say: %v %+v", err, errText(res))
	}
	cfg := r.Config(t)
	cfg.Raw.Query.Image = oxigraphImage
	cfg.Raw.Index.Kind = "oxigraph"
	keys, err := trustedKeys(cfg)
	if err != nil {
		t.Fatal(err)
	}
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })

	for _, q := range []string{
		`PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?a ?k ?u WHERE { ?a a prov:Activity . OPTIONAL { ?a prov:wasAttributedTo ?k } OPTIONAL { ?a prov:used ?u } }`,
		`SELECT ?g ?s ?p ?o WHERE { GRAPH ?g { ?s ?p ?o } }`,
		`SELECT ?s ?p ?o WHERE { ?s ?p ?o }`,
		`PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?g ?a ?k WHERE { GRAPH ?g { ?a prov:wasAttributedTo ?k } }`,
	} {
		dir := t.TempDir()
		if err := projectRegistries(ctx, cfg, keys, dir); err != nil {
			t.Fatal(err)
		}
		must(t, os.WriteFile(filepath.Join(dir, "query.rq"), []byte(q), 0o644))
		must(t, os.Mkdir(filepath.Join(dir, "store"), 0o755))
		if _, err := container.RunTool(ctx, cfg, oxigraphImage, dir, "load", "--location", "/q/store",
			"--file", "/q/default.nt", "--file", "/q/named.nq"); err != nil {
			t.Fatal(err)
		}
		res, err := container.RunTool(ctx, cfg, oxigraphImage, dir, "query", "--location", "/q/store",
			"--query-file", "/q/query.rq", "--results-format", "csv")
		if err != nil {
			t.Fatal(err)
		}
		want := rowsOf(t, res)
		a, err := oxigraph.Query(ctx, d, cfg, keys, q)
		if err != nil {
			t.Fatal(err)
		}
		got := sorted(a.Rows)
		if len(want) == 0 {
			t.Fatalf("%s: the reference answered nothing, which proves nothing", q)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s:\nindex     %v\ncontainer %v", q, got, want)
		}
	}
}

func rowsOf(t *testing.T, csvText string) []string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		m := map[string]string{}
		for j, v := range row {
			m[rows[0][j]] = v
		}
		out = append(out, m)
	}
	return sorted(out)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func sorted(rows []map[string]string) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprint(r))
	}
	sort.Strings(out)
	return out
}
