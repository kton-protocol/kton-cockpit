//go:build docker && unreleased

package oxigraph_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The index reads registries read-only, and a read-only open numbers merged records in memory only.
// A writer later numbers them in its own order (sorted paths), so a cursor taken from memory could
// lie past a record that sorts earlier, and the next incremental read would skip it for good. The
// cursor is only ever a position the store wrote down. (Found by kton-review on PR #11.)
func TestOxigraph_ACursorNeverSkipsARecordAWriterNumbersLater(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	alice := testrepo.New(t)
	ac := indexed(t, alice)
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	author(t, alice, ac, "a")

	carol := testrepo.New(t)
	cc := carol.Config(t)
	src := filepath.Join(carol.Root, "registry", "plankton", "objects")
	dst := filepath.Join(alice.Root, "registry", "plankton", "objects")
	list := func() map[string]bool {
		m := map[string]bool{}
		filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && filepath.Base(p) != ".format" {
				rel, _ := filepath.Rel(src, p)
				m[rel] = true
			}
			return nil
		})
		return m
	}
	type rec struct{ file, id string }
	var recs []rec
	for _, n := range []string{"c1", "c2", "c3", "c4"} {
		before := list()
		id := author(t, carol, cc, n)
		for f := range list() {
			if !before[f] {
				recs = append(recs, rec{f, id})
			}
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].file < recs[j].file })
	early, late := recs[0], recs[len(recs)-1]
	plant := func(r rec) {
		b, _ := os.ReadFile(filepath.Join(src, r.file))
		os.MkdirAll(filepath.Dir(filepath.Join(dst, r.file)), 0o755)
		if err := os.WriteFile(filepath.Join(dst, r.file), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	trust := append(keys(t, ac), keys(t, cc)...)
	ask := func() *oxigraph.Answer {
		a, err := oxigraph.Query(ctx, d, ac, trust, activities)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	ask()

	plant(late) // a merge: unnumbered, the reader numbers it in memory and keeps that as its cursor
	a1 := ask()
	plant(early)                   // another merge, sorting before the first
	g := author(t, alice, ac, "g") // a writer numbers both, in sorted-path order, then g
	a2 := ask()
	if !holds(a1, "a", late.id) {
		t.Fatalf("a merged record is not in the next answer")
	}
	for name, id := range map[string]string{"the earlier merge": early.id, "the later merge": late.id, "the new record": g} {
		if !holds(a2, "a", id) {
			t.Errorf("%s (%s) is in the registry but not in the answer", name, id)
		}
	}
}
