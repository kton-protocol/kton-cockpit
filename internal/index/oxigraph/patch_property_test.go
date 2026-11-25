//go:build docker

package oxigraph_test

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	nregistry "kton.dev/nekton/registry"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// ADR-022 D1.5, the guard: a view patched by what arrived equals, as sets of triples, a full build of the
// same registries. A world of signed records is made once (seeds with a parent, chained claims of three
// signers, replacements of every kind, co-signatures, unscoped claims); then random sequences deliver
// them to three registries (the own one and two federation sources) in random order and at random steps,
// and after every step the view the patch made is held against a fresh full build.

const (
	statusTemplate = `{"name": "%s", "target": "either", "predicate": "https://example.test/v/%s", "replace": "previous",
  "fields": {"state": {"type": "%s", "required": true}}}`
)

type pitem struct {
	id     string // sha256:<hex>
	env    core.Envelope
	name   string
	twinOf string // the claim a co-signature repeats, by name
	isSeed bool
	args   claimArgs
}

type claimArgs struct {
	tmpl, subject, scope, prev, state, replaces, when string
	key                                               int
}

type world struct {
	t      *testing.T
	x      *testrepo.Repo
	keys   []string // key files: trusted, trusted, untrusted
	items  []*pitem
	byName map[string]*pitem
	n      int
}

func (w *world) nek(args ...string) string {
	w.t.Helper()
	cmd := exec.Command(filepath.Join(w.x.Bin, "nekton"), args...)
	cmd.Dir = w.x.Root
	cmd.Env = append(os.Environ(), "NEKTON_DIR="+filepath.Join(w.x.Root, config.Dir, "nekton"),
		"NEKTON_TEMPLATES="+filepath.Join(w.x.Root, config.Dir, "templates"))
	out, err := cmd.Output()
	if err != nil {
		w.t.Fatalf("nekton %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (w *world) when() string {
	w.n++
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(w.n) * time.Second).Format(time.RFC3339)
}

func (w *world) read(file string) core.Envelope {
	w.t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		w.t.Fatal(err)
	}
	var env core.Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		w.t.Fatal(err)
	}
	return env
}

func (w *world) seed(name, parent string, key int) *pitem {
	w.t.Helper()
	out := filepath.Join(w.t.TempDir(), name+".dsse.json")
	args := []string{"seed", name, "--sign", w.keys[key], "--print-id", "--when", w.when(), "-o", out}
	if parent != "" {
		args = append(args, "--parent", parent)
	}
	it := &pitem{id: w.nek(args...), name: name, isSeed: true}
	it.env = w.read(out)
	w.add(it)
	return it
}

func (w *world) claim(name string, a claimArgs) *pitem {
	w.t.Helper()
	if a.when == "" {
		a.when = w.when()
	}
	out := filepath.Join(w.t.TempDir(), name+".dsse.json")
	args := []string{"annotate", a.subject, "--template", a.tmpl, "--set", "state=" + a.state,
		"--sign", w.keys[a.key], "--by", "world", "--print-id", "--when", a.when, "-o", out}
	if a.scope != "" {
		args = append(args, "--scope", a.scope, "--prev", a.prev)
	}
	if a.replaces != "" {
		args = append(args, "--set", "replaces="+a.replaces)
	}
	it := &pitem{id: w.nek(args...), name: name, args: a}
	it.env = w.read(out)
	w.add(it)
	return it
}

// cosign signs the same statement again with another key: the same claim id, one more signature.
func (w *world) cosign(name string, of *pitem, key int) *pitem {
	a := of.args
	a.key = key
	it := w.claim(name, a)
	it.twinOf = of.name
	if it.id != of.id {
		w.t.Fatalf("a co-signature of %s has another id: %s vs %s", of.name, it.id, of.id)
	}
	return it
}

func (w *world) add(it *pitem) {
	w.items = append(w.items, it)
	if it.twinOf == "" {
		w.byName[it.name] = it
	}
}

// build makes the world: a prologue that has every case the patch must get right, and a random tail.
func buildWorld(t *testing.T, rnd *rand.Rand) *world {
	x := testrepo.New(t)
	for _, tm := range []struct{ name, typ string }{{"status", "string"}, {"status2", "string"}, {"unknown", "string"}} {
		path := filepath.Join(x.Root, config.Dir, "templates", tm.name+".json")
		if err := os.WriteFile(path, []byte(fmt.Sprintf(statusTemplate, tm.name, tm.name, tm.typ)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{t: t, x: x, byName: map[string]*pitem{}}
	w.keys = []string{x.Key(testrepo.SessionID + "-claims"), filepath.Join(x.Keys, "k2.key"), filepath.Join(x.Keys, "k3.key")}
	w.nek("keygen", filepath.Join(x.Keys, "k2"))
	w.nek("keygen", filepath.Join(x.Keys, "k3"))

	s1 := w.seed("s1", "", 0)
	s2 := w.seed("s2", s1.id, 1)
	s3 := w.seed("s3", "", 2) // its seed no trusted key signed
	last := map[string]string{s1.id: s1.id, s2.id: s2.id, s3.id: s3.id}
	n := 0
	say := func(name, scope string, key int, tmpl, subject string, replaces string) *pitem {
		n++
		a := claimArgs{tmpl: tmpl, subject: subject, scope: scope, state: strconv.Itoa(n), key: key, replaces: replaces}
		if scope != "" {
			a.prev = last[scope]
		}
		it := w.claim(name, a)
		if scope != "" {
			last[scope] = it.id
		}
		return it
	}
	a1 := say("a1", s1.id, 0, "status", "urn:example:s1", "")
	a2 := say("a2", s1.id, 1, "status", "urn:example:s1", "")
	say("r1", s1.id, 0, "status", "urn:example:s1", a1.id)  // same signer replaces
	say("r2", s1.id, 0, "status", "urn:example:s1", a2.id)  // another signer: no retirement
	say("r3", s1.id, 0, "status2", "urn:example:s1", a1.id) // another template
	say("b1", s2.id, 0, "status", "urn:example:s2", "")
	say("b2", s2.id, 1, "unknown", "urn:example:s2", "")
	say("c1", s3.id, 0, "status", "urn:example:s3", "")
	u1 := say("u1", "", 0, "status", "urn:example:u", "")
	u2 := say("u2", "", 2, "status", "urn:example:u", "") // an untrusted claim ...
	w.cosign("u2co", u2, 0)                               // ... that a trusted key signs too: it becomes visible
	w.cosign("a1co", a1, 1)
	say("u3", "", 0, "status", "urn:example:u", u1.id)

	// A random tail: more claims in random scopes (a branch when it chains to an earlier one), replacements,
	// co-signatures, another seed under a scope.
	scopes := []*pitem{s1, s2, s3}
	if rnd.IntN(2) == 0 {
		s4 := w.seed("s4", s2.id, 0)
		last[s4.id] = s4.id
		scopes = append(scopes, s4)
	}
	status := []*pitem{a1, a2}
	for i := 0; i < 6+rnd.IntN(5); i++ {
		name := "t" + strconv.Itoa(i)
		switch rnd.IntN(5) {
		case 0:
			old := status[rnd.IntN(len(status))]
			tm := []string{"status", "status2"}[rnd.IntN(2)]
			sc := scopes[rnd.IntN(len(scopes))]
			status = append(status, say(name, sc.id, rnd.IntN(3), tm, old.args.subject, old.id))
		case 1:
			var cands []*pitem
			for _, it := range w.items {
				if it.twinOf == "" && it.args.tmpl != "" && it.args.key != 0 {
					cands = append(cands, it)
				}
			}
			if len(cands) > 0 {
				w.cosign(name+"co", cands[rnd.IntN(len(cands))], 0)
			}
		case 2:
			say(name, "", rnd.IntN(3), "status", "urn:example:u", "")
		default:
			sc := scopes[rnd.IntN(len(scopes))]
			if rnd.IntN(4) == 0 { // chained to an earlier claim of the scope: a branch
				var in []string
				for _, it := range w.items {
					if it.args.scope == sc.id {
						in = append(in, it.id)
					}
				}
				if len(in) > 0 {
					last[sc.id] = in[rnd.IntN(len(in))]
				}
			}
			status = append(status, say(name, sc.id, rnd.IntN(3), "status", "urn:example:"+strconv.Itoa(rnd.IntN(3)), ""))
		}
	}
	return w
}

type delivery struct {
	item *pitem
	reg  int
}

func hexPub(t *testing.T, keyFile string) string {
	t.Helper()
	b, err := os.ReadFile(strings.TrimSuffix(keyFile, ".key") + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

type harness struct {
	t   *testing.T
	ctx context.Context
	d   *oxigraph.Daemon
	cfg *config.Config
	ks  []ed25519.PublicKey
	log []string
}

func (h *harness) note(s string) { h.log = append(h.log, s) }

func (h *harness) fail(format string, a ...any) {
	h.t.Helper()
	h.t.Fatalf("%s\nsteps so far:\n  %s", fmt.Sprintf(format, a...), strings.Join(h.log, "\n  "))
}

// step brings the view up to date and holds it against a full build of the same registries, built into a
// fresh slot (a trusted key nobody signed with makes another slot, and reads every registry whole).
// want says which path the view should take: "patch" (no full build), "full" (one, and no patch).
func (h *harness) step(label, want string) {
	h.t.Helper()
	h.note(label + " -> " + want)
	f0, p0 := oxigraph.Builds()
	got, err := h.d.Prepare(h.ctx, h.cfg, h.ks)
	if err != nil {
		h.fail("%s: %v", label, err)
	}
	f1, p1 := oxigraph.Builds()
	switch want {
	case "patch":
		if f1 != f0 {
			h.fail("%s: a full build (%q), want the patch path", label, oxigraph.LastFallback())
		}
	case "full":
		if f1 != f0+1 || p1 != p0 {
			h.fail("%s: %d full and %d patch builds, want one full", label, f1-f0, p1-p0)
		}
	}
	pub, _, _ := ed25519.GenerateKey(crand.Reader)
	fresh, err := h.d.Prepare(h.ctx, h.cfg, append(append([]ed25519.PublicKey{}, h.ks...), pub))
	if err != nil {
		h.fail("%s: the full build: %v", label, err)
	}
	if fresh.Slot == got.Slot {
		h.fail("%s: the full build is in the same slot", label)
	}
	h.same(label, got, fresh)
}

func (h *harness) same(label string, a, b *oxigraph.PreparedView) {
	h.t.Helper()
	norm := func(slot, s string) string { return strings.ReplaceAll(s, slot, "SLOT") }
	graphs := func(v *oxigraph.PreparedView) map[string]map[string]bool {
		names, err := h.d.ViewGraphs(h.ctx, v.Slot)
		if err != nil {
			h.fail("%s: %v", label, err)
		}
		out := map[string]map[string]bool{}
		for _, g := range names {
			qs, err := h.d.GraphQuads(h.ctx, g)
			if err != nil {
				h.fail("%s: %v", label, err)
			}
			set := map[string]bool{}
			for _, q := range qs {
				set[norm(v.Slot, oxigraph.QuadKey(q))] = true
			}
			out[norm(v.Slot, g)] = set
		}
		return out
	}
	ga, gb := graphs(a), graphs(b)
	for g, sa := range ga {
		sb := gb[g]
		if sb == nil {
			h.fail("%s: graph %s is in the patched view and not in the full build", label, g)
		}
		diff(h, label, g, "patched", "full", sa, sb)
		diff(h, label, g, "full", "patched", sb, sa)
	}
	for g := range gb {
		if ga[g] == nil {
			h.fail("%s: graph %s is in the full build and not in the patched view", label, g)
		}
	}
	lines := func(v *oxigraph.PreparedView) map[string]bool {
		out := map[string]bool{}
		for _, n := range v.Named {
			out[norm(v.Slot, "named "+n)] = true
		}
		for _, n := range v.Scopes {
			out[norm(v.Slot, "scope "+n)] = true
		}
		for _, g := range v.Graphs {
			b, _ := json.Marshal(g)
			out[norm(v.Slot, "graph "+string(b))] = true
		}
		return out
	}
	diff(h, label, "the kept dataset", "patched", "full", lines(a), lines(b))
	diff(h, label, "the kept dataset", "full", "patched", lines(b), lines(a))
	if len(a.Graphs) != len(b.Graphs) || len(a.Named) != len(b.Named) {
		h.fail("%s: the kept dataset has %d/%d graphs and %d/%d named graphs (patched/full)", label, len(a.Graphs), len(b.Graphs), len(a.Named), len(b.Named))
	}
	for i := range a.Graphs {
		x, _ := json.Marshal(a.Graphs[i])
		y, _ := json.Marshal(b.Graphs[i])
		if norm(a.Slot, string(x)) != norm(b.Slot, string(y)) {
			h.fail("%s: the kept graphs differ in order or content at %d: %s vs %s", label, i, x, y)
		}
	}
}

func diff(h *harness, label, g, an, bn string, a, b map[string]bool) {
	h.t.Helper()
	var only []string
	for k := range a {
		if !b[k] {
			only = append(only, k)
		}
	}
	if len(only) > 0 {
		sort.Strings(only)
		only = only[:min(len(only), 4)]
		var other []string
		for k := range b {
			if !a[k] {
				other = append(other, k)
			}
		}
		sort.Strings(other)
		if len(other) > 0 {
			only = append(only, "AGAINST the "+bn+" view's:")
			only = append(only, other[:min(len(other), 4)]...)
		}
		h.fail("%s: graph %s: %d triples in the %s view and not in the %s one (first: %s)", label, g, len(only), an, bn, strings.Join(only, "\n   "))
	}
}

func TestOxigraph_APatchedViewIsAFullBuild(t *testing.T) {
	seeds := []uint64{1}
	if s := os.Getenv("COCKPIT_PROP_SEED"); s != "" {
		n, _ := strconv.ParseUint(s, 10, 64)
		seeds = []uint64{n}
	} else {
		now := uint64(time.Now().UnixNano())
		seeds = append(seeds, now%1000003, (now/7)%1000003)
	}
	for _, seed := range seeds {
		t.Run("seed"+strconv.FormatUint(seed, 10), func(t *testing.T) { propertyRun(t, seed, nil) })
	}
}

// The cases the patch has a rule for, each in the one order that needs it (the random sequences find them
// by chance; these do not leave it to chance). A step is the deliveries made before the view is read again,
// written record@registry (A the own registry, B and C the federation's sources).
func TestOxigraph_APatchedViewNeedsEachNeighbourAndEachArrival(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps [][]string
	}{
		{"a co-signature of a replaced claim ends the replacement's effect",
			[][]string{{"s1@A", "a1@A", "a2@A"}, {"r1@A", "r3@A"}, {"a1co@A"}, {"r2@A"}}},
		{"a replacement held before the claim it replaces, which arrives in another registry",
			[][]string{{"u3@A"}, {"u1@B"}, {"u2@C"}, {"u2co@A"}}},
		{"a seed arriving in a second registry names it among its scope's sources",
			[][]string{{"s1@A", "a1@A"}, {"s1@B"}, {"s1@C", "s2@A"}, {"s2@B", "b1@C"}}},
		{"a claim every registry holds waiting is admitted by its seed arriving in another, with its successors",
			[][]string{{"a1@A", "a2@B", "r1@C"}, {"s1@A"}, {"r2@B", "r3@B"}}},
		{"a claim of an untrusted key that a trusted key signs too becomes visible, old as its record is",
			[][]string{{"s3@A", "c1@A"}, {"u2@B", "u1@A"}, {"u2co@C"}, {"u3@A"}}},
	} {
		t.Run(tc.name, func(t *testing.T) { propertyRun(t, 1, tc.steps) })
	}
}

func propertyRun(t *testing.T, seed uint64, script [][]string) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	rnd := rand.New(rand.NewPCG(seed, seed*7+1))
	w := buildWorld(t, rnd)

	a, b, c := testrepo.New(t), testrepo.New(t), testrepo.New(t)
	// A trusts the world's two keys and the plankton keys of B and C.
	self := filepath.Join(a.Root, config.Dir, "keys", "self")
	for name, key := range map[string]string{"w1": w.keys[0], "w2": w.keys[1]} {
		if err := os.WriteFile(filepath.Join(self, name+".pub"), []byte(hexPub(t, key)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, r := range map[string]*testrepo.Repo{"pb": b, "pc": c} {
		if err := os.WriteFile(filepath.Join(self, name+".pub"), []byte(hexPub(t, r.Key(testrepo.SessionID))+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tmplA := filepath.Join(a.Root, config.Dir, "templates")
	writeStatus := func(typ string) {
		for _, n := range []string{"status", "status2"} {
			if err := os.WriteFile(filepath.Join(tmplA, n+".json"), []byte(fmt.Sprintf(statusTemplate, n, n, typ)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeStatus("string")
	ac, bc, cc := indexed(t, a), indexed(t, b), indexed(t, c)
	for name, r := range map[string]*testrepo.Repo{"b": b, "c": c} {
		var spec source.Spec
		sb, _ := json.Marshal(map[string]string{"name": name, "kind": "dir", "path": r.Root})
		if err := json.Unmarshal(sb, &spec); err != nil {
			t.Fatal(err)
		}
		ac.Raw.Federation.Sources = append(ac.Raw.Federation.Sources, spec)
	}
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	h := &harness{t: t, ctx: ctx, d: d, cfg: ac, ks: keys(t, ac)}
	dirs := []string{ac.NektonDir, bc.NektonDir, cc.NektonDir}
	names := []string{"A", "B", "C"}
	repos := []*testrepo.Repo{a, b, c}
	cfgs := []*config.Config{ac, bc, cc}
	t.Logf("seed %d: %d records in the world", seed, len(w.items))

	// Every record reaches at least one registry, some reach two (a record reaching a second registry is
	// nothing new to the view); the order is random.
	var queue []delivery
	for _, it := range w.items {
		first := rnd.IntN(3)
		queue = append(queue, delivery{it, first})
		// A seed reaches two registries always (a scope's source names every registry that holds its seed).
		if rnd.IntN(3) == 0 || it.isSeed {
			queue = append(queue, delivery{it, (first + 1 + rnd.IntN(2)) % 3})
		}
	}
	rnd.Shuffle(len(queue), func(i, j int) { queue[i], queue[j] = queue[j], queue[i] })
	// A registry that holds a record twice (a co-signature before its claim reaches the same registry) is the
	// kernel's to merge; delivering a record to a registry twice is not done.
	seen := map[string]bool{}
	var plan []delivery
	for _, dv := range queue {
		k := dv.item.name + "@" + strconv.Itoa(dv.reg)
		if !seen[k] {
			seen[k] = true
			plan = append(plan, dv)
		}
	}

	h.step("the empty federation", "full")
	if script != nil {
		at := map[string]int{"A": 0, "B": 1, "C": 2}
		for _, group := range script {
			for _, dv := range group {
				name, reg, _ := strings.Cut(dv, "@")
				it := w.byName[name]
				if it == nil {
					for _, x := range w.items {
						if x.name == name {
							it = x
						}
					}
				}
				r, err := nregistry.Open(dirs[at[reg]])
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := r.Add(it.env); err != nil {
					t.Fatalf("delivering %s: %v", dv, err)
				}
			}
			h.step(strings.Join(group, " "), "patch")
		}
		return
	}
	templateAt := rnd.IntN(len(plan)/2) + len(plan)/4
	plankton := map[int]bool{rnd.IntN(len(plan)): true, rnd.IntN(len(plan)): true}
	typ := "string"
	for i := 0; i < len(plan); {
		n := 1 + rnd.IntN(3)
		var label []string
		for j := 0; j < n && i < len(plan); j, i = j+1, i+1 {
			dv := plan[i]
			reg, err := nregistry.Open(dirs[dv.reg])
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := reg.Add(dv.item.env); err != nil {
				t.Fatalf("delivering %s to %s: %v", dv.item.name, names[dv.reg], err)
			}
			label = append(label, dv.item.name+"->"+names[dv.reg])
			if plankton[i] {
				author(t, repos[dv.reg], cfgs[dv.reg], "p"+strconv.Itoa(i))
				label = append(label, "plankton@"+names[dv.reg])
			}
		}
		h.step(strings.Join(label, " "), "patch")
		if i >= templateAt && typ == "string" {
			// A change the view's fingerprint covers (a template field's type: the values read as integers):
			// the view is built whole. (The alias-only change is #265's fingerprint fix, not on this branch.)
			typ = "integer"
			writeStatus(typ)
			h.step("a template field type changes", "full")
		}
	}
	h.step("the last step again", "patch")
}
