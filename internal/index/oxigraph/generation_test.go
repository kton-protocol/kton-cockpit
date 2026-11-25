//go:build docker

package oxigraph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A registry that loses records is one a view built from it cannot be patched against (ADR-022 D1.4): the
// read that finds them gone bumps the registry's generation, and the view, marked with the generation it
// was built at, is built whole, and equals a build into a fresh slot.
func TestOxigraph_ARegistryThatLostRecordsIsBuiltWholeNotPatched(t *testing.T) {
	s := newSHWorld(t)
	h := &harness{t: t, ctx: context.Background(), d: s.d, cfg: s.ac, ks: keys(t, s.ac)}
	seed := s.w.seed("s", "", 0)
	other := s.w.seed("o", "", 0)
	c1 := s.claim("c1", seed, seed, 0, "status")
	c2 := s.claim("c2", seed, c1, 0, "status")
	o1 := s.claim("o1", other, other, 0, "status")
	s.deliver("A", seed, c1)
	s.deliver("B", other)
	h.step("built", "full")
	s.deliver("A", c2)
	s.deliver("B", o1)
	h.step("two records arrive", "patch")

	// B loses its scope's file: the read finds records gone.
	if err := os.Remove(filepath.Join(nd(s.b), "objects", "scope", strings.TrimPrefix(other.id, "sha256:")+".nekton.jsonl")); err != nil {
		t.Fatal(err)
	}
	h.step("a registry lost its records", "full")
	h.step("and nothing since", "patch")
}
