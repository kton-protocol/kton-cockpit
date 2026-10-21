//go:build docker && unreleased

package oxigraph_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The Oxigraph image the daemon runs, pinned by digest like every image the cockpit starts.
const image = "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"

const (
	activities = `PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?a WHERE { ?a a prov:Activity }`
	attributed = `PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?a WHERE { ?a prov:wasAttributedTo ?k }`
	claims     = `SELECT DISTINCT ?g WHERE { GRAPH ?g { ?s ?p ?o } }`
)

func indexed(t *testing.T, r *testrepo.Repo) *config.Config {
	t.Helper()
	cfg := r.Config(t)
	cfg.Raw.Query.Image = image
	cfg.Raw.Index.Kind = "oxigraph"
	return cfg
}

func author(t *testing.T, r *testrepo.Repo, cfg *config.Config, name string) string {
	t.Helper()
	r.Write(t, "data/"+name+".in", name+"\n")
	r.Write(t, "data/"+name+".out", name+" done\n")
	a, err := binaries.New(cfg).Author(context.Background(), binaries.AuthorInput{
		Inputs: []string{"data/" + name + ".in"}, Outputs: []string{"data/" + name + ".out"},
		Cmd: "do " + name, SignKey: cfg.PlanktonKey})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func keys(t *testing.T, cfg *config.Config) []ed25519.PublicKey {
	t.Helper()
	k, err := binaries.TrustKeys(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func query(t *testing.T, d *oxigraph.Daemon, cfg *config.Config, q string) *oxigraph.Answer {
	t.Helper()
	a, err := oxigraph.Query(context.Background(), d, cfg, keys(t, cfg), q)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func holds(a *oxigraph.Answer, col, id string) bool {
	want := "https://kton.dev/o/" + strings.TrimPrefix(id, "sha256:")
	for _, row := range a.Rows {
		if row[col] == want {
			return true
		}
	}
	return false
}

// One daemon holds the view of each repository under its trusted keys. A view is projected again
// when its registries change and kept while they do not; a query sees its own view and nothing of
// another, so what one repository trusts never shows in another's answer.
func TestOxigraph_ViewsAreKeptWhileCurrentAndNeverSeeEachOther(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	alice := testrepo.New(t)
	ac := indexed(t, alice)
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })

	first := author(t, alice, ac, "a")
	if _, err := binaries.New(ac).Annotate(ctx, first, "working-on", map[string]string{"step": "checking", "by-session": testrepo.SessionID}, ac.NektonKey, nil); err != nil {
		t.Fatal(err)
	}
	a := query(t, d, ac, activities)
	if a.Kept || !holds(a, "a", first) {
		t.Fatalf("the first query projects the view and finds the record: %+v", a)
	}
	query(t, d, ac, activities) // opening a registry the first time may write to it
	if a := query(t, d, ac, activities); !a.Kept || !holds(a, "a", first) {
		t.Fatalf("with nothing changed, the view is kept: %+v", a)
	}
	if c := query(t, d, ac, claims); len(c.Rows) == 0 || !strings.HasPrefix(c.Rows[0]["g"], "https://kton.dev/np/") {
		t.Fatalf("the claim is read as the kernel's nanopublication: %+v", c.Rows)
	}

	second := author(t, alice, ac, "b")
	if a := query(t, d, ac, activities); a.Kept || !holds(a, "a", second) {
		t.Fatalf("a new record is in the next answer: %+v", a)
	}

	// bob reads alice's registry but trusts only himself: alice's records are in his view, but
	// attributed to nobody, and her claim is not projected at all.
	bob := testrepo.New(t)
	bc := indexed(t, bob)
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "alice", "kind": "dir", "path": alice.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	bc.Raw.Federation.Sources = []source.Spec{spec}
	if a := query(t, d, bc, activities); !holds(a, "a", first) {
		t.Fatalf("bob's view holds alice's records: %+v", a)
	}
	if a := query(t, d, bc, attributed); holds(a, "a", first) {
		t.Fatalf("bob's view attributes a record to a key he does not trust: %+v", a)
	}
	if c := query(t, d, bc, claims); len(c.Rows) != 0 {
		t.Fatalf("bob's view holds a claim no key he trusts signed: %+v", c.Rows)
	}
	if a := query(t, d, ac, attributed); !holds(a, "a", first) || !a.Kept {
		t.Fatalf("alice's view is unchanged by bob's: %+v", a)
	}
	// A query that calls the daemon itself would read bob's view from alice's; it is refused.
	escape := `SELECT ?g WHERE { SERVICE <http://127.0.0.1:7878/query> { GRAPH ?g { ?s ?p ?o } } }`
	if _, err := oxigraph.Query(ctx, d, ac, keys(t, ac), escape); !errors.Is(err, index.ErrService) {
		t.Fatalf("a SERVICE query was not refused: %v", err)
	}
	if n, err := d.Views(ctx); err != nil || n != 2 {
		t.Fatalf("the daemon holds %d views (%v), want 2", n, err)
	}

	// A configuration that wants another image does not use this daemon.
	other := *ac
	other.Raw.Query.Image = "oci://ghcr.io/oxigraph/oxigraph@sha256:" + strings.Repeat("0", 64)
	if _, err := oxigraph.Ensure(ctx, &other); !errors.Is(err, oxigraph.ErrMismatch) {
		t.Fatalf("a daemon of another image was used: %v", err)
	}

	// Emptying it loses nothing: the next query projects again.
	if err := d.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if a := query(t, d, ac, activities); a.Kept || !holds(a, "a", second) {
		t.Fatalf("after rebuild the view is projected again: %+v", a)
	}
}
