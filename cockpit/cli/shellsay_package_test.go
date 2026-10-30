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

// The templates an allowed package brings are allowed (SPEC §8.1), so say lists them beside the
// configured ones; and a flag typed beside the JSON form is refused with the field that carries it.
func TestShellSay_ListsAnAllowedPackagesTemplatesAndPointsFlagsToTheJSONFields(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "templates"), 0o755))
	must(os.WriteFile(filepath.Join(src, "templates", "requests.json"), []byte(`{"name": "requests", "target": "either",
  "predicate": "https://example.org/ln/requests", "fields": {"lane": {"type": "string", "required": true}}}`), 0o644))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	pkg := filepath.Join(t.TempDir(), "inbox")
	_, _, err = scope.BuildTemplates(pkg, "inbox", "v1", filepath.Join(src, "templates"), "", key, scope.Options{})
	must(err)
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(context.Background(), ".")
	must(err)
	var names []string
	for _, tmpl := range readTemplates(cfg) {
		names = append(names, tmpl.Name)
	}
	if !strings.Contains(" "+strings.Join(names, " ")+" ", " requests ") {
		t.Errorf("the package's template is not listed: %v", names)
	}

	err = shellSay(context.Background(), []string{"--as", "lane-a", `{"template": "requests"}`})
	if err == nil || !strings.Contains(err.Error(), `"as"`) {
		t.Errorf("--as beside the JSON form: want the field named, got %v", err)
	}
}
