package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A package's own aliases resolve its own templates only: its predicate reaches the shared set as a full
// IRI, its terms and prefixes never enter that set, an iri value written in its prefix means what it
// says at say, and a package that names template short names is refused (SPEC §9.11, kton-review A1
// on #164; userinterface's S6 (2)).
func TestAliases_APackagesOwnAliasesResolveOnlyItsOwnTemplates(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	approves := func(name string) string {
		return `{"name": "` + name + `", "target": "either", "predicate": "q:approves",
  "fields": {"outcome": {"type": "string"}, "target": {"type": "iri"}}}`
	}
	buildPkg(t, filepath.Join(t.TempDir(), "one"), "one", "v1", key, map[string]string{
		"approves-one": approves("approves-one"),
		"aliases":      `{"prefixes": {"q": "https://ex.org/one/"}, "terms": {"outcome": "q:outcome"}}`,
	})
	buildPkg(t, filepath.Join(t.TempDir(), "two"), "two", "v1", key, map[string]string{
		"approves-two": approves("approves-two"),
		"aliases":      `{"prefixes": {"q": "https://ex.org/two/"}}`,
	})
	buildPkg(t, filepath.Join(t.TempDir(), "three"), "three", "v1", key, map[string]string{
		"approves-three": `{"name": "approves-three", "target": "either", "predicate": "https://ex.org/three", "fields": {"note": {"type": "string"}}}`,
		"aliases":        `{"templates": {"approves-one": "approves-three"}}`,
	})

	cfg, err := config.Load(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	set, own, err := binaries.New(cfg).TemplatesAndAliases()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"approves-one": "https://ex.org/one/approves", "approves-two": "https://ex.org/two/approves"} {
		if tm, ok := set.Get(name); !ok || tm.Predicate != want {
			t.Errorf("%s's predicate is %q, want %q through its own package's prefix", name, tm.Predicate, want)
		}
	}
	if got := set.Resolve("q:x"); got != "q:x" {
		t.Errorf("a package's prefix entered the shared set: q:x resolves to %q", got)
	}
	if got := set.Resolve("outcome"); got != "outcome" {
		t.Errorf("a package's term entered the shared set: outcome resolves to %q", got)
	}
	if got := own["approves-one"].Resolve("outcome"); got != "https://ex.org/one/outcome" {
		t.Errorf("approves-one's field does not resolve through its package's term: %q", got)
	}
	if got := own["approves-two"].Resolve("outcome"); got != "outcome" {
		t.Errorf("package one's term resolves package two's field: %q", got)
	}

	// A package naming template short names is refused, so it cannot redirect another's say.
	if tm, ok := set.Get("approves-one"); !ok || tm.Name != "approves-one" {
		t.Errorf("approves-one was redirected to %q", tm.Name)
	}
	if _, ok := set.Get("approves-three"); ok {
		t.Error("a package with template short names still contributes its templates")
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, "template short names") {
		t.Errorf("doctor does not name the refused package:\n%s", out)
	}

	// An iri value in a package's prefix is that package's IRI at say.
	for _, name := range []string{"approves-one", "approves-two"} {
		if out, code := r.Cockpit(t, "say", name, "urn:example:thing", "target=q:thing", "outcome=ok"); code != 0 {
			t.Errorf("say %s with target=q:thing (exit %d):\n%s", name, code, out)
		}
	}
}
