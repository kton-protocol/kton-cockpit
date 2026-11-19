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
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A broken package directory nobody admitted takes nothing away: the admitted package's templates
// stay allowed and listed (kton-review U2 on #152). Before, one unreadable directory made every
// package template refuse.
func TestAllowed_ABrokenUnadmittedPackageTakesNothingAway(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "templates"), 0o755)
	os.WriteFile(filepath.Join(src, "templates", "requests.json"), []byte(`{"name": "requests", "target": "either",
  "predicate": "https://example.org/ln/requests", "fields": {"lane": {"type": "string"}}}`), 0o644)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	pkg := filepath.Join(t.TempDir(), "inbox")
	if _, _, err := scope.BuildTemplates(pkg, "inbox", "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true}); err != nil {
		t.Fatal(err)
	}
	r.Write(t, ".kton/packages/broken@v1/nekton/garbage.jsonl", "not a store\n")

	cfg, err := config.Load(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tmpl := range readTemplates(cfg) {
		names = append(names, tmpl.Name)
	}
	if !strings.Contains(" "+strings.Join(names, " ")+" ", " requests ") {
		t.Fatalf("a broken unadmitted package hid the admitted one's template: %v", names)
	}
}

// Every form of say takes an IRI subject (SPEC §8), the shell form too.
func TestShellSay_TakesAnIRISubject(t *testing.T) {
	r := testrepo.New(t)
	if out, code := r.Cockpit(t, "say", "working-on", "urn:example:thing", "step=checking"); code != 0 || !strings.Contains(out, "about urn:example:thing") {
		t.Fatalf("say about an IRI (exit %d):\n%s", code, out)
	}
}
