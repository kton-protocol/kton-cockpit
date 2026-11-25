//go:build docker

package oxigraph_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

const (
	nkV = "https://kton.dev/v/"
	oV  = "https://kton.dev/o/"
)

// A fully resolved chain builds as it did before ADR-022: every trusted claim's node carries its
// position (the seed 0, a claim one more than its predecessor, an untrusted claim in the chain
// counted), and the chain graph holds every claim of the chain, trusted or not, with the same
// positions. The chain graph is in no query's dataset.
func TestOxigraph_AFullyResolvedChainBuildsAsBefore(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	seed := r.SeedScope(t, "s")
	c1 := r.ChainClaimDirectly(t, "urn:example:x", seed, seed, "one")
	stranger := r.ChainClaimAsStranger(t, "urn:example:x", seed, c1, "two")
	c3 := r.ChainClaimDirectly(t, "urn:example:x", seed, stranger, "three")
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	ks := keys(t, cfg)
	hex := func(id string) string { return strings.TrimPrefix(id, "sha256:") }

	ans, err := oxigraph.Free(ctx, d, cfg, ks, `SELECT ?c ?n ?prev WHERE { ?c a <`+nkV+`Claim> ; <`+nkV+`position> ?n . OPTIONAL { ?c <`+nkV+`prev> ?prev } }`, oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range ans.Rows {
		got[row["c"]] = row["n"] + " " + row["prev"]
	}
	want := map[string]string{
		oV + hex(seed): "0 ",
		oV + hex(c1):   "1 " + oV + hex(seed),
		oV + hex(c3):   "3 " + oV + hex(stranger), // the untrusted claim between them counts
	}
	// The fixture holds scopes of its own besides this one; only this chain is compared.
	for h, w := range want {
		if got[h] != w {
			t.Errorf("claim node %s: position and prev %q, want %q", h, got[h], w)
		}
	}
	if _, ok := got[oV+hex(stranger)]; ok {
		t.Errorf("a claim node for the untrusted claim: %q", got[oV+hex(stranger)])
	}

	slot, err := oxigraph.SlotFor(ctx, cfg, ks)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.IndexRows(ctx, `SELECT ?c ?n WHERE { GRAPH <`+oxigraph.ChainIRI(slot)+`> { ?c <`+nkV+`position> ?n } }`)
	if err != nil {
		t.Fatal(err)
	}
	chain := map[string]string{}
	for _, row := range rows {
		chain[row["c"]] = row["n"]
	}
	wantChain := map[string]string{oV + hex(seed): "0", oV + hex(c1): "1", oV + hex(stranger): "2", oV + hex(c3): "3"}
	for h, w := range wantChain {
		if chain[h] != w {
			t.Errorf("chain graph: position of %s is %q, want %q", h, chain[h], w)
		}
	}
	rows, err = d.IndexRows(ctx, `SELECT ?g WHERE { GRAPH <`+oxigraph.ChainIRI(slot)+`> { <`+oV+hex(seed)+`> <`+nkV+`genesis> ?g } }`)
	if err != nil || len(rows) != 1 {
		t.Errorf("the seed in the chain graph: genesis %v (%v)", rows, err)
	}
	// In no query's dataset: no scope graph or the claims union lists it.
	ans, err = oxigraph.Free(ctx, d, cfg, ks, `SELECT (COUNT(*) AS ?n) WHERE { GRAPH ?g { ?s <`+nkV+`position> ?o } FILTER(CONTAINS(STR(?g), ":chain")) }`, oxigraph.DatasetClaims, time.Minute)
	if err != nil || ans.Rows[0]["n"] != "0" {
		t.Errorf("the chain graph is in the claims dataset: %v (%v)", ans.Rows, err)
	}
}

// A claim the kernel held waiting for its seed is recorded as deferred, and as resolved, no longer
// deferred, once the seed arrived and the registry was read again from its cursor. The kernel's
// settleDeferred does not append a settling claim to the feed, so the index also asks about every claim
// it recorded as waiting by id (chainview.go previouslyDeferred). In this flow the kernel's writer gives
// a waiting claim a fresh number on each pass, so the claim comes back in Records(cursor) by itself; no
// fixture was found where it does not, so this test holds the outcome, not the by-id question.
func TestOxigraph_AClaimThatSettlesLaterIsRecordedAsResolved(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	x := testrepo.New(t)
	seed := x.SeedScope(t, "s")
	c1 := x.ChainClaimDirectly(t, "urn:example:x", seed, seed, "one")
	objects := func(r *testrepo.Repo) string { return filepath.Join(r.Root, ".kton", "nekton", "objects", "scope") }
	file := strings.TrimPrefix(seed, "sha256:") + ".nekton.jsonl"
	raw, err := os.ReadFile(filepath.Join(objects(x), file))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("the scope file of X holds %d records, want the seed and one claim", len(lines))
	}

	b := testrepo.New(t)
	bc := indexed(t, b)
	// B has numbered records of its own already, so what its writer numbers later continues that
	// numbering (the epoch stays) and the read after it goes on from the cursor.
	own := b.SeedScope(t, "own")
	b.ChainClaimDirectly(t, "urn:example:y", own, own, "first")
	if err := os.MkdirAll(objects(b), 0o755); err != nil {
		t.Fatal(err)
	}
	// B holds the claim and not its seed: the kernel keeps it waiting.
	if err := os.WriteFile(filepath.Join(objects(b), file), append(append([]byte{}, lines[1]...), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := oxigraph.Ensure(ctx, bc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), bc) })
	xc := indexed(t, x)
	ks := append(keys(t, bc), keys(t, xc)...)
	read := func() {
		t.Helper()
		if _, err := oxigraph.Free(ctx, d, bc, ks, `SELECT ?s WHERE { ?s a <`+nkV+`Claim> }`, oxigraph.DatasetClaims, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	state := func(p string) int {
		t.Helper()
		rows, err := d.IndexRows(ctx, `SELECT ?c WHERE { GRAPH <urn:kton:index> { <`+oxigraph.RegIRI(bc.NektonDir)+`> <urn:kton:`+p+`> <`+oV+strings.TrimPrefix(c1, "sha256:")+`> . BIND(1 AS ?c) } }`)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	read()
	if state("defers") != 1 || state("resolves") != 0 {
		t.Fatalf("B holds the claim without its seed: defers=%d resolves=%d, want 1 and 0", state("defers"), state("resolves"))
	}

	// Every record graph and attribution graph B brought is numbered once, from 1, without gaps (ADR-022 D1.1).
	rows, err := d.IndexRows(ctx, `SELECT ?n WHERE { GRAPH <urn:kton:index> { ?g <urn:kton:arrived:`+strings.TrimPrefix(oxigraph.RegIRI(bc.NektonDir), "urn:kton:reg:")+`> ?n } }`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r["n"]] = true
	}
	if len(rows) == 0 || len(seen) != len(rows) {
		t.Errorf("arrival numbers of B's registry: %d rows, %d distinct", len(rows), len(seen))
	}
	for i := 1; i <= len(rows); i++ {
		if !seen[fmt.Sprint(i)] {
			t.Errorf("arrival number %d is missing of %d", i, len(rows))
		}
	}

	// B's writer runs while the claim waits, and the read after it moves the cursor on.
	b.ChainClaimDirectly(t, "urn:example:y", own, own, "second")
	read()
	if state("defers") != 1 {
		t.Fatalf("the claim still waits: defers=%d, want 1", state("defers"))
	}

	// The seed arrives, and B's writer numbers it (with a claim of its own scope); the next read goes on
	// from the cursor.
	if err := os.WriteFile(filepath.Join(objects(b), file), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(objects(x))
	for _, e := range entries {
		if e.Name() == file {
			continue
		}
		bs, err := os.ReadFile(filepath.Join(objects(x), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(objects(b), e.Name()), bs, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b.ChainClaimDirectly(t, "urn:example:y", own, own, "mine")
	read()
	if state("defers") != 0 || state("resolves") != 1 {
		t.Errorf("the seed arrived: defers=%d resolves=%d, want 0 and 1", state("defers"), state("resolves"))
	}
}

// A registry that loses records is one a view built from it cannot be patched against: the index counts
// the times a read found something gone, per registry (ADR-022 D1.4).
func TestOxigraph_AReadThatFindsRecordsGoneCountsAGeneration(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	b := testrepo.New(t)
	bc := indexed(t, b)
	own := b.SeedScope(t, "own")
	b.ChainClaimDirectly(t, "urn:example:y", own, own, "one")
	d, err := oxigraph.Ensure(ctx, bc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), bc) })
	read := func() {
		t.Helper()
		if _, err := oxigraph.Free(ctx, d, bc, keys(t, bc), `SELECT ?s WHERE { ?s a <`+nkV+`Claim> }`, oxigraph.DatasetClaims, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	generation := func() int {
		t.Helper()
		rows, err := d.IndexRows(ctx, `SELECT ?n WHERE { GRAPH <urn:kton:index> { <`+oxigraph.RegIRI(bc.NektonDir)+`> <urn:kton:generation> ?n } }`)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return 0
		}
		return int(rows[0]["n"][0] - '0')
	}
	read()
	if g := generation(); g != 0 {
		t.Fatalf("nothing gone yet, generation %d", g)
	}
	if err := os.Remove(filepath.Join(b.Root, ".kton", "nekton", "objects", "scope", strings.TrimPrefix(own, "sha256:")+".nekton.jsonl")); err != nil {
		t.Fatal(err)
	}
	read()
	if g := generation(); g != 1 {
		t.Errorf("records gone: generation %d, want 1", g)
	}
}
