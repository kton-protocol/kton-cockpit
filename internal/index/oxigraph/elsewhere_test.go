//go:build docker

package oxigraph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nregistry "kton.dev/nekton/registry"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A claim whose scope seed, or whose chain predecessor, is held only in ANOTHER registry of the same
// federation must still be in the index: the union of the federation settles it (nregistry.OpenUnion
// of both directories), although each registry read alone leaves it waiting. The index reads each
// registry alone (store.go apply), so this asks whether it loses such a claim.
func TestOxigraph_ClaimWhoseChainIsHeldElsewhere(t *testing.T) {
	for _, tc := range []struct {
		name string
		// which of X's records (seed, c1, c2) go to A and to B
		a, b []int
		// the record the union holds and B alone does not
		claim int
		// deferred in every registry: the record's graphs stay in the store, but no claim node is made
		// of it (ADR-022: a claim is a node only when its chain resolves in the view's registries)
		everywhere bool
	}{
		{"seed held only in another registry", []int{0}, []int{1}, 1, false},
		{"predecessor held only in another registry", []int{0, 1}, []int{2}, 2, false},
		{"claim deferred in every registry (a nanopub graph, no claim node)", nil, []int{1}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())

			// X: the scratch registry the records are made in (seed, c1 chained to the seed, c2 chained to c1).
			x := testrepo.New(t)
			xc := indexed(t, x)
			seed := x.SeedScope(t, "s")
			c1 := x.ChainClaimDirectly(t, "urn:example:x", seed, seed, "one")
			c2 := x.ChainClaimDirectly(t, "urn:example:x", seed, c1, "two")
			ids := []string{seed, c1, c2}
			scopeFile := filepath.Join(x.Root, ".kton", "nekton", "objects", "scope", strings.TrimPrefix(seed, "sha256:")+".nekton.jsonl")
			raw, err := os.ReadFile(scopeFile)
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
			for i, id := range ids {
				if i >= len(lines) || !bytes.Contains(lines[i], []byte(`"claimId":"`+id+`"`)) {
					t.Fatalf("the scope file of X does not hold record %d (%s) on line %d", i, id, i+1)
				}
			}

			// A and B each get a plain file of some of the records, as a pull of part of a scope file brings them.
			// The root scope's file (the seed's parent) goes with the seed to the registry that holds the seed.
			objects := func(r *testrepo.Repo) string { return filepath.Join(r.Root, ".kton", "nekton", "objects", "scope") }
			give := func(r *testrepo.Repo, which []int, withRoot bool) {
				t.Helper()
				if len(which) == 0 {
					return
				}
				var buf []byte
				for _, i := range which {
					buf = append(buf, lines[i]...)
					buf = append(buf, '\n')
				}
				if err := os.MkdirAll(objects(r), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(objects(r), strings.TrimPrefix(seed, "sha256:")+".nekton.jsonl"), buf, 0o644); err != nil {
					t.Fatal(err)
				}
				if withRoot {
					entries, _ := os.ReadDir(objects(x))
					for _, e := range entries {
						if strings.HasPrefix(e.Name(), strings.TrimPrefix(seed, "sha256:")) {
							continue
						}
						b, err := os.ReadFile(filepath.Join(objects(x), e.Name()))
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(objects(r), e.Name()), b, 0o644); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			a := testrepo.New(t)
			ac := indexed(t, a)
			b := testrepo.New(t)
			bc := indexed(t, b)
			give(a, tc.a, true)
			give(b, tc.b, false)

			// B has A as a federation source and trusts X's signer.
			var spec source.Spec
			sb, _ := json.Marshal(map[string]string{"name": "a", "kind": "dir", "path": a.Root})
			if err := json.Unmarshal(sb, &spec); err != nil {
				t.Fatal(err)
			}
			bc.Raw.Federation.Sources = append(bc.Raw.Federation.Sources, spec)
			bk := append(keys(t, bc), keys(t, xc)...)

			// Sanity: the kernel's union of A and B holds the claim; B alone does not.
			want := ids[tc.claim]
			hex := strings.TrimPrefix(want, "sha256:")
			union, err := nregistry.OpenUnion(ac.NektonDir, bc.NektonDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := union.Claim(want); ok == tc.everywhere {
				t.Fatalf("setup: the union of A and B holds %s = %v, want %v", want, ok, !tc.everywhere)
			}
			if _, _, def := union.DeferredClaim(want); def != tc.everywhere {
				t.Fatalf("setup: the union reports %s deferred = %v, want %v", want, def, tc.everywhere)
			}
			alone, err := nregistry.OpenUnion(bc.NektonDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := alone.Claim(want); ok {
				t.Fatalf("setup: B alone already holds %s, so nothing is held elsewhere", want)
			}
			_, waiting, deferred := alone.DeferredClaim(want)
			t.Logf("B alone: claim %s deferred=%v waitingOnScope=%q", want, deferred, waiting)

			// The index of B.
			d, err := oxigraph.Ensure(ctx, bc)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { oxigraph.Stop(context.Background(), bc) })
			free := func(dataset, q string) []map[string]string {
				t.Helper()
				ans, err := oxigraph.Free(ctx, d, bc, bk, q, dataset, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				return ans.Rows
			}
			node := free(oxigraph.DatasetClaims, `SELECT ?c WHERE { ?c a <https://kton.dev/v/Claim> }`)
			gotNode := false
			for _, r := range node {
				if r["c"] == "https://kton.dev/o/"+hex {
					gotNode = true
				}
			}
			np := free(oxigraph.DatasetNanopub, `SELECT ?g ?s WHERE { GRAPH ?g { ?s ?p ?o } FILTER(CONTAINS(STR(?g), "`+hex+`") || CONTAINS(STR(?s), "`+hex+`")) } LIMIT 5`)
			if tc.everywhere {
				if gotNode {
					t.Errorf("claims dataset: a node <https://kton.dev/o/%s> (a Claim) although no registry of the federation resolves its chain; it holds %d claim nodes", hex, len(node))
				}
				if len(np) == 0 {
					t.Errorf("nanopub dataset: no graph or subject naming %s: the record is data like any imported graph and stays", hex)
				}
				// Nor in any scope graph, and no scope metadata counts it (nk:claims).
				inGraph := free(oxigraph.DatasetClaims, `SELECT ?g WHERE { GRAPH ?g { <https://kton.dev/o/`+hex+`> ?p ?o } }`)
				if len(inGraph) != 0 {
					t.Errorf("claims dataset: the claim has triples in %d scope graphs, want none", len(inGraph))
				}
				counted := free(oxigraph.DatasetClaims, `SELECT ?g ?n WHERE { ?g <https://kton.dev/v/scope> <https://kton.dev/o/`+strings.TrimPrefix(ids[0], "sha256:")+`> ; <https://kton.dev/v/claims> ?n }`)
				if len(counted) != 0 {
					t.Errorf("claims dataset: scope metadata for the seed nobody holds, counting %v claims", counted)
				}
				// Nor does it stand in a scope graph of its own: its seed is nobody's.
				scopes := free(oxigraph.DatasetClaims, `SELECT ?g WHERE { ?g <https://kton.dev/v/scope> ?s }`)
				for _, r := range scopes {
					if strings.Contains(r["s"], strings.TrimPrefix(ids[0], "sha256:")) {
						t.Errorf("claims dataset: a scope graph %s for the seed nobody holds", r["g"])
					}
				}
				return
			}
			if !gotNode {
				t.Errorf("claims dataset: the index of B holds no node <https://kton.dev/o/%s> (a Claim) although the federation's union holds the claim; it holds %d claim nodes", hex, len(node))
			}
			if len(np) == 0 {
				t.Errorf("nanopub dataset: the index of B holds no graph or subject naming %s although the federation's union holds the claim", hex)
			}
		})
	}
}
