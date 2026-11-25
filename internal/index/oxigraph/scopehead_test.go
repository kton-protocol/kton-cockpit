//go:build docker

package oxigraph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	nregistry "kton.dev/nekton/registry"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// shWorld is a world of signed records (patch_property_test.go's) and two registries of a federation: A
// the own registry and B a source. Records are delivered to a registry by hand, so each scenario holds
// exactly the arrangement it names.
type shWorld struct {
	t    *testing.T
	w    *world
	a, b *testrepo.Repo
	ac   *config.Config
	d    *oxigraph.Daemon
}

func newSHWorld(t *testing.T) *shWorld {
	t.Helper()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	x := testrepo.New(t)
	for _, tm := range []struct{ name, pred string }{{"status", "https://example.test/v/status"}, {"badpred", "https://example.test/v/has space"}} {
		body := fmt.Sprintf(`{"name": "%s", "target": "either", "predicate": "%s", "replace": "previous",
  "fields": {"state": {"type": "string", "required": true}}}`, tm.name, tm.pred)
		if err := os.WriteFile(filepath.Join(x.Root, config.Dir, "templates", tm.name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{t: t, x: x, byName: map[string]*pitem{}}
	w.keys = []string{x.Key(testrepo.SessionID + "-claims"), filepath.Join(x.Keys, "k2.key"), filepath.Join(x.Keys, "k3.key")}
	w.nek("keygen", filepath.Join(x.Keys, "k2"))
	w.nek("keygen", filepath.Join(x.Keys, "k3"))
	a, b := testrepo.New(t), testrepo.New(t)
	self := filepath.Join(a.Root, config.Dir, "keys", "self")
	for name, key := range map[string]string{"w1": w.keys[0], "w2": w.keys[1]} { // k3 stays untrusted
		if err := os.WriteFile(filepath.Join(self, name+".pub"), []byte(hexPub(t, key)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ac := indexed(t, a)
	var spec source.Spec
	sb, _ := json.Marshal(map[string]string{"name": "b", "kind": "dir", "path": b.Root})
	if err := json.Unmarshal(sb, &spec); err != nil {
		t.Fatal(err)
	}
	ac.Raw.Federation.Sources = append(ac.Raw.Federation.Sources, spec)
	d, err := oxigraph.Ensure(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	return &shWorld{t: t, w: w, a: a, b: b, ac: ac, d: d}
}

func (s *shWorld) claim(name string, scope, prev *pitem, key int, tmpl string) *pitem {
	s.t.Helper()
	a := claimArgs{tmpl: tmpl, subject: "urn:example:sh", state: name, key: key}
	if scope != nil {
		a.scope, a.prev = scope.id, prev.id
	}
	return s.w.claim(name, a)
}

func (s *shWorld) deliver(reg string, its ...*pitem) {
	s.t.Helper()
	dir := s.ac.NektonDir
	if reg == "B" {
		dir = nd(s.b)
	}
	r, err := nregistry.Open(dir)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, it := range its {
		if _, _, err := r.Add(it.env); err != nil {
			s.t.Fatalf("delivering %s to %s: %v", it.name, reg, err)
		}
	}
}

// union is the kernel's own answer: the union of every nekton directory of the slot.
func (s *shWorld) union() *nregistry.Registry {
	s.t.Helper()
	in, err := binaries.Inputs(context.Background(), s.ac)
	if err != nil {
		s.t.Fatal(err)
	}
	var dirs []string
	for _, i := range in {
		if binaries.Exists(i.NektonDir) {
			dirs = append(dirs, i.NektonDir)
		}
	}
	u, err := nregistry.OpenUnion(dirs...)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// same holds the index's head of scope against the union's.
func (s *shWorld) same(label string, scope *pitem) {
	s.t.Helper()
	ks := keys(s.t, s.ac)
	u := s.union()
	heads, length, ok := u.Heads(scope.id)
	got, err := oxigraph.ScopeHead(context.Background(), s.d, s.ac, ks, scope.id)
	if !ok {
		if err == nil {
			s.t.Errorf("%s: the union has no such scope, the index answered %+v", label, got)
		}
		return
	}
	if err != nil {
		s.t.Fatalf("%s: %v", label, err)
	}
	if !slices.Equal(got.Heads, heads) || got.ChainLength != length || got.Unresolved != u.Unresolved(scope.id) || got.Branched != (len(heads) > 1) {
		s.t.Errorf("%s: index heads %v length %d unresolved %d branched %v; union heads %v length %d unresolved %d",
			label, got.Heads, got.ChainLength, got.Unresolved, got.Branched, heads, length, u.Unresolved(scope.id))
	}
}

// The head the index reads from its chain graph is the head the kernel reads from the union of the
// registries (ADR-022 D3), in each arrangement of a scope's records the two could differ on.
func TestOxigraph_TheIndexHeadIsTheUnionsHead(t *testing.T) {
	type scenario struct {
		name  string
		build func(s *shWorld) *pitem // delivers the records, returns the scope's seed
	}
	for _, sc := range []scenario{
		{"a linear scope", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 1, "status")
			c3 := s.claim("c3", seed, c2, 0, "status")
			s.deliver("A", seed, c1, c2, c3)
			return seed
		}},
		{"a branched scope", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 0, "status")
			c3 := s.claim("c3", seed, c1, 1, "status") // the same prev as c2
			s.deliver("A", seed, c1, c2, c3)
			return seed
		}},
		{"the seed in a federation source and the claims in the own registry", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 0, "status")
			s.deliver("B", seed)
			s.deliver("A", c1, c2)
			return seed
		}},
		{"a prev in another registry", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 0, "status")
			c3 := s.claim("c3", seed, c2, 1, "status")
			s.deliver("A", seed, c2)
			s.deliver("B", c1, c3)
			return seed
		}},
		{"a claim deferred in every registry in a scope that has resolved claims", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 0, "status") // in no registry
			c3 := s.claim("c3", seed, c2, 0, "status")
			c4 := s.claim("c4", seed, c2, 1, "status")
			s.deliver("A", seed, c1, c3)
			s.deliver("B", c4)
			return seed
		}},
		{"an untrusted key's claim as the last in a scope", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 2, "status")
			s.deliver("A", seed, c1, c2)
			return seed
		}},
		{"a claim whose predicate the projection percent-encodes, last in a scope", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			c2 := s.claim("c2", seed, c1, 0, "badpred")
			s.deliver("A", seed, c1, c2)
			return seed
		}},
		{"an empty scope", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			s.deliver("A", seed)
			return seed
		}},
		{"a scope whose seed is in no registry", func(s *shWorld) *pitem {
			seed := s.w.seed("s", "", 0)
			c1 := s.claim("c1", seed, seed, 0, "status")
			s.deliver("A", c1)
			return seed
		}},
	} {
		t.Run(sc.name, func(t *testing.T) {
			s := newSHWorld(t)
			seed := sc.build(s)
			s.same("first read", seed)
			// A claim arriving after the view was built is in the next head.
			s.same("again", seed)
		})
	}
}

// The scenario with a percent-encoded last claim must really be one: the kernel projects the claim (the
// space of its predicate becomes %20), the claims dataset holds a node for it and for the valid claim
// before it, and the chain counts both. The query reads the claims dataset: the default view graph holds
// no claim node at all, so a query of it would return nothing for either claim and prove nothing.
//
// kton#307 will make the kernel refuse a template with such a predicate at load. The fixture then needs
// another trigger for a claim whose predicate the projection rewrites (or the scenario goes).
func TestOxigraph_ThePercentEncodedClaimIsInTheChainAndInTheProjection(t *testing.T) {
	s := newSHWorld(t)
	seed := s.w.seed("s", "", 0)
	c1 := s.claim("c1", seed, seed, 0, "status")
	c2 := s.claim("c2", seed, c1, 0, "badpred")
	s.deliver("A", seed, c1, c2)
	got, err := oxigraph.ScopeHead(context.Background(), s.d, s.ac, keys(t, s.ac), seed.id)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{c2.id}; !slices.Equal(got.Heads, want) || got.ChainLength != 2 {
		t.Fatalf("heads %v length %d, want %v and 2", got.Heads, got.ChainLength, want)
	}
	ans, err := oxigraph.Free(context.Background(), s.d, s.ac, keys(t, s.ac),
		`SELECT ?c ?p WHERE { ?c a <`+nkV+`Claim> ; <`+nkV+`predicate> ?p }`, oxigraph.DatasetClaims, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The guard: the query sees the valid claim, so an empty answer for c2 cannot pass for a finding.
	has := func(id string) bool {
		for _, row := range ans.Rows {
			if row["c"] == "https://kton.dev/o/"+strings.TrimPrefix(id, "sha256:") {
				return true
			}
		}
		return false
	}
	if !has(c1.id) {
		t.Fatalf("the claims dataset holds no node for the valid claim c1: the query is blind (%d rows)", len(ans.Rows))
	}
	if !has(c2.id) {
		t.Fatalf("the claims dataset holds no node for c2 (%d rows)", len(ans.Rows))
	}
	want := "https://kton.dev/o/" + strings.TrimPrefix(c2.id, "sha256:")
	for _, row := range ans.Rows {
		if row["c"] == want {
			if row["p"] != "https://example.test/v/has%20space" {
				t.Fatalf("c2's predicate is %q, want the percent-encoded https://example.test/v/has%%20space", row["p"])
			}
			return
		}
	}
}

// Random arrangements: the claims of three scopes (branches, untrusted keys, a claim with a percent-encoded predicate) reach two
// registries in random order and the view is read at random steps; at each the index's head of every
// scope is the union's.
func TestOxigraph_TheIndexHeadFollowsTheUnionThroughDeliveries(t *testing.T) {
	for seed := uint64(1); seed <= 3; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			rnd := rand.New(rand.NewPCG(seed, seed*7+1))
			s := newSHWorld(t)
			var seeds []*pitem
			var all []*pitem
			for i := 0; i < 3; i++ {
				sd := s.w.seed(fmt.Sprint("s", i), "", 0)
				seeds = append(seeds, sd)
				all = append(all, sd)
				members := []*pitem{sd}
				for j := 0; j < 5; j++ {
					prev := members[rnd.IntN(len(members))]
					tmpl := "status"
					if rnd.IntN(8) == 0 {
						tmpl = "badpred"
					}
					c := s.claim(fmt.Sprint("c", i, j), sd, prev, []int{0, 1, 2}[rnd.IntN(3)], tmpl)
					members = append(members, c)
					all = append(all, c)
				}
			}
			rnd.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
			for i, it := range all {
				s.deliver([]string{"A", "B"}[rnd.IntN(2)], it)
				if i%4 == 3 || i == len(all)-1 {
					for _, sd := range seeds {
						s.same(fmt.Sprint("after ", i+1, " records, scope ", sd.name), sd)
					}
				}
			}
		})
	}
}
