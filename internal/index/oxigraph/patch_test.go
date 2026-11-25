//go:build docker

package oxigraph_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	nclaim "kton.dev/nekton/claim"
	nregistry "kton.dev/nekton/registry"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A say into a view of a few hundred claims is a patch: the view is not built whole, the claim projection
// reads the graphs of the one claim (not of the three hundred), the scope graph and the rest are the graphs
// that moved, and the chain graph, the index's own bookkeeping, is not named to a frontend (ADR-022 D1).
func TestOxigraph_ASayIntoALargeViewIsOnePatch(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	seed := r.SeedScope(t, "big")
	keyBytes, err := os.ReadFile(r.Key(testrepo.SessionID + "-claims"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := hex.DecodeString(strings.TrimSpace(string(keyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	priv := ed25519.NewKeyFromSeed(s)
	reg, err := nregistry.Open(cfg.NektonDir)
	if err != nil {
		t.Fatal(err)
	}
	prev := seed
	const claims = 300
	for i := 0; i < claims; i++ {
		env, id, err := nclaim.SignWith(nclaim.Spec{
			Subject:   []nclaim.SubjectSpec{{URI: "urn:example:big"}},
			Predicate: "https://kton.dev/v/working-on",
			Object:    map[string]any{"step": strconv.Itoa(i), "by-session": "bulk"},
			By:        "bulk", When: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339),
			Scope: seed, Prev: prev,
		}, priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := reg.Add(env); err != nil {
			t.Fatal(err)
		}
		prev = id
	}
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	ks := keys(t, cfg)

	first, err := d.Prepare(ctx, cfg, ks)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Named) < 4*claims {
		t.Fatalf("setup: %d named graphs for %d claims", len(first.Named), claims)
	}
	before, err := d.ReadViewState(ctx, first.Slot)
	if err != nil {
		t.Fatal(err)
	}

	r.ChainClaimDirectly(t, "urn:example:big", seed, prev, "say")
	f0, p0 := oxigraph.Builds()
	read0 := oxigraph.QuadGraphsRead()
	if _, err := d.Prepare(ctx, cfg, ks); err != nil {
		t.Fatal(err)
	}
	f1, p1 := oxigraph.Builds()
	if f1 != f0 || p1 != p0+1 {
		t.Errorf("after one say: %d full and %d patch builds, want one patch (fell back: %q)", f1-f0, p1-p0, oxigraph.LastFallback())
	}
	if n := oxigraph.QuadGraphsRead() - read0; n > 12 {
		t.Errorf("the claim projection read %d graphs for one claim of %d, want a handful", n, claims)
	}

	after, err := d.ReadViewState(ctx, first.Slot)
	if err != nil {
		t.Fatal(err)
	}
	c := after.Diff(before)
	if c.All {
		t.Fatalf("the two states cannot be compared: %+v", c)
	}
	moved := strings.Join(c.Graphs, " ")
	if !strings.Contains(moved, ":scope:"+strings.TrimPrefix(seed, "sha256:")) || !strings.Contains(moved, ":rest") {
		t.Errorf("a say into a scope names its graph and the rest, got %v", c.Graphs)
	}
	if len(c.Graphs) != 2 || len(c.Removed) != 0 {
		t.Errorf("a say moves the scope's graph and the rest and nothing else, got %v removed %v", c.Graphs, c.Removed)
	}
	if strings.Contains(moved, ":chain") {
		t.Errorf("the chain graph is the index's own bookkeeping, named to a frontend: %v", c.Graphs)
	}
	if _, in := after.Graphs[oxigraph.ChainIRI(first.Slot)]; in {
		t.Errorf("the view state lists the chain graph")
	}
}

// A claim a registry holds waiting is a claim node, with its position and its successors, once the record it
// waits for arrives in another registry of the federation, and that arrival is a patch: the union settles
// the waiting claims whose seed or predecessor is in the delta (ADR-022 build decision 2).
func TestOxigraph_AWaitingClaimIsAdmittedWhenWhatItWaitsForArrivesElsewhere(t *testing.T) {
	for _, tc := range []struct {
		name string
		// the records of the scope (0 seed, 1..3 claims) that A holds first, and those that arrive in B later
		a, b []int
	}{
		{"its predecessor arrives in the second registry", []int{2, 3}, []int{0, 1}},
		{"its seed arrives in the second registry", []int{1, 2, 3}, []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
			x := testrepo.New(t)
			seed := x.SeedScope(t, "s")
			c1 := x.ChainClaimDirectly(t, "urn:example:x", seed, seed, "one")
			c2 := x.ChainClaimDirectly(t, "urn:example:x", seed, c1, "two")
			c3 := x.ChainClaimDirectly(t, "urn:example:x", seed, c2, "three")
			ids := []string{seed, c1, c2, c3}
			xreg, err := nregistry.Open(nd(x))
			if err != nil {
				t.Fatal(err)
			}
			a, b := testrepo.New(t), testrepo.New(t)
			self := filepath.Join(a.Root, config.Dir, "keys", "self")
			if err := os.WriteFile(filepath.Join(self, "x.pub"), []byte(hexPub(t, x.Key(testrepo.SessionID+"-claims"))+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ac := indexed(t, a)
			var spec source.Spec
			sb, _ := json.Marshal(map[string]string{"name": "b", "kind": "dir", "path": b.Root})
			if err := json.Unmarshal(sb, &spec); err != nil {
				t.Fatal(err)
			}
			ac.Raw.Federation.Sources = append(ac.Raw.Federation.Sources, spec)
			deliver := func(dir string, which []int) {
				t.Helper()
				reg, err := nregistry.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, i := range which {
					rec, ok := xreg.Claim(ids[i])
					if !ok {
						t.Fatalf("X does not hold %s", ids[i])
					}
					if _, _, err := reg.Add(rec.Envelope); err != nil {
						t.Fatal(err)
					}
				}
			}
			deliver(ac.NektonDir, tc.a)
			// B holds something of its own, so it has records that numbered before.
			b.SeedScope(t, "other")
			d, err := oxigraph.Ensure(ctx, ac)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
			ks := keys(t, ac)
			pv, err := d.Prepare(ctx, ac, ks)
			if err != nil {
				t.Fatal(err)
			}
			nodes := func() map[string]string {
				t.Helper()
				qs, err := d.GraphQuads(ctx, "urn:kton:view:"+pv.Slot+":claims")
				if err != nil {
					t.Fatal(err)
				}
				out := map[string]string{}
				for _, q := range qs {
					if q.P.Value == nkV+"position" {
						out[strings.TrimPrefix(q.S.Value, oV)] = q.O.Value
					}
				}
				return out
			}
			for _, i := range tc.a {
				if _, in := nodes()[strings.TrimPrefix(ids[i], "sha256:")]; in && i != 0 {
					t.Fatalf("setup: %s is a node before what it waits for arrived", ids[i])
				}
			}

			deliver(nd(b), tc.b)
			f0, p0 := oxigraph.Builds()
			if _, err := d.Prepare(ctx, ac, ks); err != nil {
				t.Fatal(err)
			}
			if f1, p1 := oxigraph.Builds(); f1 != f0 || p1 != p0+1 {
				t.Fatalf("B's delta: %d full and %d patch builds, want one patch (fell back: %q)", f1-f0, p1-p0, oxigraph.LastFallback())
			}
			got := nodes()
			for i, id := range ids {
				if p := got[strings.TrimPrefix(id, "sha256:")]; p != strconv.Itoa(i) {
					t.Errorf("record %d (%s): position %q, want %d", i, id[:15], p, i)
				}
			}
		})
	}
}

func nd(r *testrepo.Repo) string { return filepath.Join(r.Root, config.Dir, "nekton") }
