package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// ui list --json lists every installed package for a frontend: name, revision, profile, whether
// the configuration admits it, and what fails its check, under refused rather than hidden
// (userinterface's PR 2, C2.4).
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
	if p := by[".kton/packages/broken@v1"]; len(p.Refused) != 1 || p.Refused[0].Code != "package.check" || p.Refused[0].Reason == "" || p.UI != nil {
		t.Errorf("the broken package is not listed with its refusal, ui null: %+v", p)
	}
	// A package that declares nothing for a frontend says so with empty lists, not null.
	if p := by[".kton/packages/plain@v1"]; p.UI == nil || len(p.UI) != 0 || p.Blocks == nil || p.Compositions == nil {
		t.Errorf("a templates package's declarations should be empty lists: %+v", p)
	}
}

// A verticle's ui/, blocks and compositions are listed as ktonpkg (v0.9) reads them, each under its
// file name, for a frontend to read instead of the package folder (C2.4). The package is ktonpkg's own
// test verticle (scope/testdata/hostile/good), read from the module ktonpkg is built from.
func TestUIList_ListsAVerticlesUIBlocksAndCompositions(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gitmick/ktonpkg").Output()
	if err != nil {
		t.Fatalf("locating ktonpkg: %v", err)
	}
	good := filepath.Join(strings.TrimSpace(string(out)), "scope", "testdata", "hostile", "good")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	dir := filepath.Join(t.TempDir(), "review")
	if _, _, err := scope.BuildVerticle(dir, "review", "v1", good, key, scope.Options{}); err != nil {
		t.Fatalf("building ktonpkg's good verticle: %v", err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name         string           `json:"name"`
			Allowed      bool             `json:"allowed"`
			UI           []map[string]any `json:"ui"`
			Blocks       []map[string]any `json:"blocks"`
			Compositions []map[string]any `json:"compositions"`
			Refused      []UIRefusal      `json:"refused"`
		} `json:"packages"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 1 {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	p := l.Packages[0]
	if p.Name != "review" || !p.Allowed || len(p.Refused) != 0 {
		t.Fatalf("the verticle: %+v", p)
	}
	if len(p.UI) != 1 || p.UI[0]["file"] != "review" || p.UI[0]["slot"] == nil {
		t.Errorf("ui entries: %v", p.UI)
	}
	if len(p.Blocks) != 1 || p.Blocks[0]["name"] != "table" || p.Blocks[0]["viewer"] == nil {
		t.Errorf("blocks: %v", p.Blocks)
	}
	if len(p.Compositions) != 1 || p.Compositions[0]["name"] != "list" || p.Compositions[0]["kind"] == nil {
		t.Errorf("compositions: %v", p.Compositions)
	}
}
