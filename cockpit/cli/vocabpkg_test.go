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
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The vocabulary is per package (userinterface's PR 2 (3), S6 (3)): a template name two admitted
// packages bring is offered only as <package>/<name>, and an admitted package that fails its check
// contributes nothing; the rest keeps working either way, and doctor says what was left out.
func TestVocabulary_AClashOrAFailingPackageTakesOutOnlyItself(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	pkg := func(name string, templates ...string) {
		src := t.TempDir()
		os.MkdirAll(filepath.Join(src, "templates"), 0o755)
		for _, tm := range templates {
			os.WriteFile(filepath.Join(src, "templates", tm+".json"), []byte(`{"name": "`+tm+`", "target": "either",
  "predicate": "https://example.org/`+name+`/`+tm+`", "fields": {"note": {"type": "string"}}}`), 0o644)
		}
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		dir := filepath.Join(t.TempDir(), name)
		if _, _, err := scope.BuildTemplates(dir, name, "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Install(ctx, cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	pkg("area-one", "approves", "deviation")
	pkg("area-two", "approves")
	subject := "urn:example:item-1"
	say := func(tmpl string) (string, error) {
		out, err := c.Say(ctx, cockpit.SayRequest{Subject: subject, Template: tmpl, Fields: map[string]string{"note": "x"}})
		if err != nil {
			return "", err
		}
		return out.ClaimID, nil
	}
	if _, err := say("approves"); err == nil {
		t.Error("a clashing name said unqualified: want refused")
	}
	for _, tmpl := range []string{"area-one/approves", "area-two/approves", "deviation"} {
		if _, err := say(tmpl); err != nil {
			t.Errorf("say %s: %v", tmpl, err)
		}
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, `"approves" is brought by area-one and area-two`) {
		t.Errorf("doctor does not name the clash:\n%s", out)
	}

	// area-two's bytes no longer match its seal: it contributes nothing, area-one keeps working.
	blobs, _ := filepath.Glob(filepath.Join(r.Root, ".kton", "packages", "area-two@v1", "blobs", "*", "*"))
	if len(blobs) == 0 {
		t.Fatal("no blobs to tamper with")
	}
	for _, b := range blobs {
		os.WriteFile(b, []byte("tampered"), 0o644)
	}
	if _, err := say("deviation"); err != nil {
		t.Errorf("one failing package took the others down: %v", err)
	}
	if _, err := say("approves"); err != nil {
		t.Errorf("with area-two out, approves is area-one's alone again: %v", err)
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, "the allowed package area-two contributes nothing") {
		t.Errorf("doctor does not name the failing package:\n%s", out)
	}
}

// '@' is reserved for the scopes install opens (<package>@<label>): a records scope cannot be
// seeded under such a name, so the two never collide (userinterface, S6).
func TestScopeSeed_RefusesANameAnInstallScopeCouldHave(t *testing.T) {
	r := testrepo.New(t)
	if out, code := r.Cockpit(t, "scope", "seed", "qa.area-one@v1"); code == 0 || !strings.Contains(out, "reserved") {
		t.Fatalf("seeding qa.area-one@v1 (exit %d):\n%s", code, out)
	}
}
