package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A package's own aliases resolve its fields in the view, merged with the repository's: the
// repository's definition wins, and a key two packages define differently is left out and named by
// doctor (userinterface's S6 (2)).
func TestAliases_APackagesOwnAliasesResolveMergedWithTheRepositorys(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	pkg := func(name, aliases string) {
		src := t.TempDir()
		os.MkdirAll(filepath.Join(src, "templates"), 0o755)
		os.WriteFile(filepath.Join(src, "templates", name+".json"), []byte(`{"name": "`+name+`", "target": "either",
  "predicate": "https://example.org/`+name+`", "fields": {"outcome": {"type": "string"}}}`), 0o644)
		// ktonpkg takes aliases.json in the templates folder as the package's alias file.
		os.WriteFile(filepath.Join(src, "templates", "aliases.json"), []byte(aliases), 0o644)
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		dir := filepath.Join(t.TempDir(), name)
		if _, _, err := scope.BuildTemplates(dir, name, "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func(term string) string {
		t.Helper()
		cfg, err := config.Load(ctx, ".")
		if err != nil {
			t.Fatal(err)
		}
		set, err := binaries.New(cfg).Templates()
		if err != nil {
			t.Fatal(err)
		}
		return set.Resolve(term)
	}
	pkg("qa-one", `{"terms": {"outcome": "https://example.org/q/outcome", "verdict": "https://example.org/q/verdict"}}`)
	if got := resolve("outcome"); got != "https://example.org/q/outcome" {
		t.Fatalf("a package's own alias does not resolve: %q", got)
	}

	// Another package defines outcome differently: left out for both, verdict still resolves.
	pkg("qa-two", `{"terms": {"outcome": "https://example.org/other/outcome"}}`)
	if got := resolve("outcome"); strings.Contains(got, "example.org") {
		t.Errorf("a term two packages define differently resolved to %q", got)
	}
	if got := resolve("verdict"); got != "https://example.org/q/verdict" {
		t.Errorf("an uncontested term stopped resolving: %q", got)
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, `aliases: the term "outcome" is defined differently`) {
		t.Errorf("doctor does not name the alias clash:\n%s", out)
	}

	// The repository's own definition wins over both.
	r.Write(t, "aliases.json", `{"terms": {"outcome": "https://example.org/repo/outcome"}}`)
	if got := resolve("outcome"); got != "https://example.org/repo/outcome" {
		t.Errorf("the repository's alias does not win: %q", got)
	}
}
