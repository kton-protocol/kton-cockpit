//go:build docker

package oxigraph_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
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

// records counts the distinct record graphs the store holds.
func records(t *testing.T, d *oxigraph.Daemon) int {
	t.Helper()
	body, err := http.PostForm(d.URL+"/query", url.Values{"query": {
		`SELECT (COUNT(DISTINCT ?r) AS ?n) WHERE { GRAPH <urn:kton:index> { ?r <urn:kton:layer> ?l } }`}})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Body.Close()
	var r struct {
		Results struct {
			Bindings []map[string]struct{ Value string } `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(body.Body).Decode(&r); err != nil || len(r.Results.Bindings) != 1 {
		t.Fatalf("counting records: %v", err)
	}
	var n int
	fmt.Sscan(r.Results.Bindings[0]["n"].Value, &n)
	return n
}

// One daemon holds every record once, whichever repositories read it, with its attribution to each
// key that verifies it. A query reads a registry again only when its position moved, and sees the
// records of its own union, attributed only to the keys its configuration trusts.
func TestOxigraph_RecordsAreStoredOnceAndTrustIsChosenPerQuery(t *testing.T) {
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
		t.Fatalf("the first query reads the registries and finds the record: %+v", a)
	}
	query(t, d, ac, activities) // opening a registry the first time numbers it
	if a := query(t, d, ac, activities); !a.Kept || !holds(a, "a", first) {
		t.Fatalf("with nothing changed, nothing is read: %+v", a)
	}
	if c := query(t, d, ac, claims); len(c.Rows) == 0 || !strings.HasPrefix(c.Rows[0]["g"], "https://kton.dev/") {
		t.Fatalf("the claim is read in the kernel's graphs: %+v", c.Rows)
	}

	second := author(t, alice, ac, "b")
	if a := query(t, d, ac, activities); a.Kept || !holds(a, "a", second) {
		t.Fatalf("a new record is in the next answer: %+v", a)
	}
	// A record that arrives as a file, as a git merge brings it, is numbered by nobody yet: the
	// position's objects move while its maxSeq does not, and the registry is read again from 0.
	carol := testrepo.New(t)
	third := author(t, carol, carol.Config(t), "c")
	query(t, d, ac, activities)
	if out, err := exec.Command("cp", "-rn", filepath.Join(carol.Root, "registry", "plankton", "objects")+"/.",
		filepath.Join(alice.Root, "registry", "plankton", "objects")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if a := query(t, d, ac, activities); a.Kept || !holds(a, "a", third) {
		t.Fatalf("a record that arrived unnumbered is not in the answer: %+v", a)
	}
	if a := query(t, d, ac, attributed); holds(a, "a", third) {
		t.Fatalf("carol's record is attributed although alice does not trust her: %+v", a)
	}
	held := records(t, d)

	// bob reads alice's registry but trusts only himself: her records are in his answer, attributed
	// to nobody, and her claim is not in it at all. They are not stored a second time.
	bob := testrepo.New(t)
	bc := indexed(t, bob)
	// He reads her registry where it is, and a copy of it elsewhere (a mirror, a source cache): another
	// registry directory with the same records.
	mirror := filepath.Join(t.TempDir(), "mirror")
	if out, err := exec.Command("cp", "-r", alice.Root, mirror).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, src := range []map[string]string{{"name": "alice", "kind": "dir", "path": alice.Root}, {"name": "mirror", "kind": "dir", "path": mirror}} {
		var spec source.Spec
		b, _ := json.Marshal(src)
		if err := json.Unmarshal(b, &spec); err != nil {
			t.Fatal(err)
		}
		bc.Raw.Federation.Sources = append(bc.Raw.Federation.Sources, spec)
	}
	if a := query(t, d, bc, activities); !holds(a, "a", first) || !holds(a, "a", second) || !holds(a, "a", third) {
		t.Fatalf("bob's answer holds alice's records: %+v", a)
	}
	if a := query(t, d, bc, attributed); holds(a, "a", first) {
		t.Fatalf("bob's answer attributes a record to a key he does not trust: %+v", a)
	}
	if c := query(t, d, bc, claims); len(c.Rows) != 0 {
		t.Fatalf("bob's answer holds a claim no key he trusts signed: %+v", c.Rows)
	}
	// Once bob trusts alice's keys, her records are attributed in his answer, the mirror's included,
	// which was read only under his own keys so far.
	if a, err := oxigraph.Query(ctx, d, bc, append(keys(t, bc), keys(t, ac)...), attributed); err != nil || a.Kept || !holds(a, "a", first) {
		t.Fatalf("trusting another key did not attribute the records already held: %+v %v", a, err)
	}
	if n := records(t, d); n != held {
		t.Fatalf("the store holds %d records after bob read alice's, %d before: stored twice", n, held)
	}
	if a := query(t, d, ac, attributed); !holds(a, "a", first) || !a.Kept {
		t.Fatalf("alice's answer is unchanged by bob's: %+v", a)
	}
	if a := query(t, d, ac, attributed); holds(a, "a", "sha256:"+strings.Repeat("0", 64)) {
		t.Fatalf("an answer holds a record of no registry it reads: %+v", a)
	}
	if n, err := d.Registries(ctx); err != nil || n != 6 {
		t.Fatalf("the daemon holds %d registries (%v), want 6: alice's, the mirror's and bob's own, two each", n, err)
	}

	// A query that calls the daemon itself would read past its dataset; it is refused.
	escape := `SELECT ?g WHERE { SERVICE <http://127.0.0.1:7878/query> { GRAPH ?g { ?s ?p ?o } } }`
	if _, err := oxigraph.Query(ctx, d, ac, keys(t, ac), escape); !errors.Is(err, index.ErrService) {
		t.Fatalf("a SERVICE query was not refused: %v", err)
	}

	// A configuration that wants another image does not use this daemon.
	other := *ac
	other.Raw.Query.Image = "oci://ghcr.io/oxigraph/oxigraph@sha256:" + strings.Repeat("0", 64)
	if _, err := oxigraph.Ensure(ctx, &other); !errors.Is(err, oxigraph.ErrMismatch) {
		t.Fatalf("a daemon of another image was used: %v", err)
	}

	// Emptying it loses nothing: the next query reads the registries again.
	if err := d.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if a := query(t, d, ac, activities); a.Kept || !holds(a, "a", second) {
		t.Fatalf("after rebuild the registries are read again: %+v", a)
	}

	// A store a cockpit of another schema wrote (phase 2's views) is emptied before it is used.
	update := func(u string) {
		t.Helper()
		resp, err := http.PostForm(d.URL+"/update", url.Values{"update": {u}})
		if err != nil || resp.StatusCode >= 300 {
			t.Fatalf("%s: %v %v", u, err, resp.Status)
		}
		resp.Body.Close()
	}
	update(`DELETE WHERE { GRAPH <urn:kton:index> { <urn:kton:index> <urn:kton:schema> ?s } }`)
	update(`INSERT DATA { GRAPH <urn:kton:view:x:plankton> { <urn:a> <urn:b> <urn:c> } }`)
	if _, err := oxigraph.Ensure(ctx, ac); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Registries(ctx); err != nil || n != 0 || records(t, d) != 0 {
		t.Fatalf("a store of another schema was used as it was: %d registries (%v)", n, err)
	}
}
