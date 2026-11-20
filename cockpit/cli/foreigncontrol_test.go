package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// goodVerticle copies ktonpkg's good verticle to a directory of its own and applies edit to it.
func goodVerticle(t *testing.T, edit func(dir string)) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gitmick/ktonpkg").Output()
	if err != nil {
		t.Fatalf("locating ktonpkg: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "src")
	if err := os.CopyFS(dst, os.DirFS(filepath.Join(strings.TrimSpace(string(out)), "scope", "testdata", "hostile", "good"))); err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error { // CopyFS keeps the module cache's read-only modes
		if err == nil {
			os.Chmod(p, map[bool]os.FileMode{true: 0o755, false: 0o644}[d.IsDir()])
		}
		return nil
	})
	edit(dst)
	return dst
}

func build(t *testing.T, name, label, src string) (dir, head string) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	dir = filepath.Join(t.TempDir(), name)
	if _, _, err := scope.BuildVerticle(dir, name, label, src, key, scope.Options{}); err != nil {
		t.Fatalf("building %s: %v", name, err)
	}
	p, err := scope.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, seal, err := p.Sealed()
	if err != nil {
		t.Fatal(err)
	}
	return dir, fmt.Sprint(seal.Object["hash"])
}

// A view that acts on another package's control (ktonpkg v0.9.13) is checked against the owner as
// installed here: install refuses it until the owner is installed at the revision it pins, refuses a
// pin to another head, and ui list refuses it once the owner is gone.
func TestForeignControl_TheOwnerMustBeInstalledAsPinned(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	owner, head := build(t, "runs", "v7", goodVerticle(t, func(dir string) {
		os.WriteFile(filepath.Join(dir, "controls", "rerun-request.json"), []byte(`{"kind":"record","say":{"template":"approves"}}`), 0o644)
	}))
	view := func(pin string) string {
		dir, _ := build(t, "review", "v1", goodVerticle(t, func(dir string) {
			p := filepath.Join(dir, "ui", "compositions", "list.json")
			b, _ := os.ReadFile(p)
			s := strings.Replace(string(b), `"empty": "show",`, `"empty": "show",
     "acts": [{"intent": "record", "control": "rerun-request", "from": {"package": "runs", "at": "v7", "head": "`+pin+`"}, "fields": {}}],`, 1)
			if s == string(b) {
				t.Fatal("the composition has no card block to act on")
			}
			os.WriteFile(p, []byte(s), 0o644)
		}))
		return dir
	}
	install := func(dir string) error {
		_, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: dir, Allow: true})
		return err
	}

	if err := install(view(head)); err == nil || !strings.Contains(err.Error(), `the package "runs" is not installed`) {
		t.Fatalf("the view before its owner: want a refusal naming the owner, got %v", err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: owner}); err != nil {
		t.Fatalf("the owner, installed but not yet admitted: %v", err)
	}
	other := "sha256:" + strings.Repeat("e", 64)
	if err := install(view(other)); err == nil || !strings.Contains(err.Error(), "from pins the head "+other) {
		t.Fatalf("a pin to another head: want a refusal naming it, got %v", err)
	}
	if err := install(view(head)); err != nil {
		t.Fatalf("the view after its owner: %v", err)
	}
	refusals := func() string {
		t.Helper()
		stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
		var l struct {
			Packages []struct {
				Name    string      `json:"name"`
				Refused []UIRefusal `json:"refused"`
			} `json:"packages"`
		}
		if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil {
			t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
		}
		for _, p := range l.Packages {
			if p.Name == "review" {
				b, _ := json.Marshal(p.Refused)
				return string(b)
			}
		}
		t.Fatalf("no review package listed: %s", stdout)
		return ""
	}
	// The act signs with the owner's template, which say takes only from an admitted package.
	if got := refusals(); !strings.Contains(got, "runs@v7 is installed but not admitted") {
		t.Errorf("with its owner installed but not admitted, ui list refuses the view: %s", got)
	}
	if err := install(owner); err != nil {
		t.Fatalf("admitting the owner: %v", err)
	}
	if got := refusals(); got != "[]" {
		t.Errorf("with its owner installed and admitted, the view is usable: %s", got)
	}
	if err := os.RemoveAll(filepath.Join(r.Root, ".kton", "packages", "runs@v7")); err != nil {
		t.Fatal(err)
	}
	if got := refusals(); !strings.Contains(got, `the package \"runs\" is not installed`) {
		t.Errorf("with its owner gone, ui list refuses the view: %s", got)
	}
}
