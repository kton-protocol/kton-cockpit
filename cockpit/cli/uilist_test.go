package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// ui list --json lists every installed package for a frontend: name, revision, profile, whether
// the configuration admits it, and what fails its check, under refused rather than hidden
// (userinterface's PR 2, C2.4). ui, blocks and compositions are absent until ktonpkg reads them.
func TestUIList_ListsInstalledPackagesWithWhatFailsTheirCheck(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	build := func(name string) string {
		src := t.TempDir()
		os.MkdirAll(filepath.Join(src, "templates"), 0o755)
		os.WriteFile(filepath.Join(src, "templates", name+".json"), []byte(`{"name": "`+name+`", "target": "either",
  "predicate": "https://example.org/`+name+`", "fields": {"note": {"type": "string"}}}`), 0o644)
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		dir := filepath.Join(t.TempDir(), name)
		if _, _, err := scope.BuildTemplates(dir, name, "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if _, err := c.Install(ctx, cockpit.InstallRequest{Path: build("admitted"), Allow: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(ctx, cockpit.InstallRequest{Path: build("plain")}); err != nil {
		t.Fatal(err)
	}
	// A package directory whose store is not a package: listed, refused, not hidden.
	r.Write(t, ".kton/packages/broken@v1/nekton/garbage.jsonl", "not a store\n")

	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l UIListing
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	by := map[string]UIPackage{}
	for _, p := range l.Packages {
		by[p.Dir] = p
	}
	if p := by[".kton/packages/admitted@v1"]; p.Name != "admitted" || !p.Allowed || p.Revision == "" || len(p.Refused) != 0 {
		t.Errorf("the admitted package: %+v", p)
	}
	if p := by[".kton/packages/plain@v1"]; p.Name != "plain" || p.Allowed || len(p.Refused) != 0 {
		t.Errorf("the installed, not admitted package: %+v", p)
	}
	if p := by[".kton/packages/broken@v1"]; len(p.Refused) != 1 || p.Refused[0].Code != "package.check" || p.Refused[0].Reason == "" {
		t.Errorf("the broken package is not listed with its refusal: %+v", p)
	}
	var raw map[string][]map[string]any
	json.Unmarshal([]byte(stdout), &raw)
	for _, p := range raw["packages"] {
		for _, k := range []string{"ui", "blocks", "compositions"} {
			if _, ok := p[k]; ok {
				t.Errorf("%s present before ktonpkg reads it: %v", k, p)
			}
		}
	}
}
