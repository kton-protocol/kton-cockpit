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

// buildPkg builds a templates package named name, revision label, from templates (file -> body),
// signed with key, into dir, and installs and admits it.
func buildPkg(t *testing.T, dir, name, label string, key ed25519.PrivateKey, templates map[string]string) {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "templates"), 0o755)
	for f, body := range templates {
		os.WriteFile(filepath.Join(src, "templates", f+".json"), []byte(body), 0o644)
	}
	if _, _, err := scope.BuildTemplates(dir, name, label, filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatalf("install %s@%s: %v", name, label, err)
	}
}

func tmpl(name, pred string) string {
	return `{"name": "` + name + `", "target": "either", "predicate": "` + pred + `", "fields": {"note": {"type": "string"}}}`
}

// The qualifier is the package's name, which a package declares itself: two admitted packages of one
// name are both refused, so qm/approves can never sign another key's predicate (kton-review V1 on
// #162). A template declaring a name with '/' or other than its file's refuses its package, not the
// vocabulary (V2). Two installed revisions of one package give the latest (V3).
func TestVocabulary_QualifiedNamesCannotBeSpoofed(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	say := func(tm string) error {
		_, err := c.Say(ctx, cockpit.SayRequest{Subject: "urn:example:x", Template: tm, Fields: map[string]string{"note": "x"}})
		return err
	}
	_, keyA, _ := ed25519.GenerateKey(rand.Reader)
	_, keyB, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "qm-genuine"), "qm", "v1", keyA, map[string]string{"approves": tmpl("approves", "https://example.org/genuine")})
	buildPkg(t, filepath.Join(t.TempDir(), "qm-impostor"), "qm", "v2", keyB, map[string]string{"approves": tmpl("approves", "https://example.org/impostor")})
	for _, tm := range []string{"approves", "qm/approves"} {
		if err := say(tm); err == nil {
			t.Errorf("say %s with two packages named qm: want refused", tm)
		}
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, "two admitted packages are named qm") {
		t.Errorf("doctor does not name the two qm packages:\n%s", out)
	}

	// V2: a template declaring 'area-one/approves' refuses its package; the others keep working.
	_, keyC, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "sly"), "sly", "v1", keyC, map[string]string{"x": tmpl("area-one/approves", "https://example.org/sly")})
	buildPkg(t, filepath.Join(t.TempDir(), "fine"), "fine", "v1", keyC, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	if err := say("notes"); err != nil {
		t.Errorf("a package declaring a qualified name took the others down: %v", err)
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, "the allowed package sly contributes nothing") {
		t.Errorf("doctor does not name sly:\n%s", out)
	}

	// V3: one package installed as v1 and as v2: the v2 template is offered, under its plain name.
	_, keyD, _ := ed25519.GenerateKey(rand.Reader)
	dir := filepath.Join(t.TempDir(), "area")
	buildPkg(t, dir, "area", "v1", keyD, map[string]string{"decides": tmpl("decides", "https://example.org/decides")})
	p, err := scope.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := p.WithSigner(keyD, scope.Options{}).NewRevision("v2")
	if err != nil {
		t.Fatal(err)
	}
	if err := rev.SetProfile(scope.ProfileTemplates); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"decides": tmpl("decides", "https://example.org/decides"), "records": tmpl("records", "https://example.org/records")} {
		if _, err := rev.AddTemplate(name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rev.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatalf("install area@v2: %v", err)
	}
	for _, d := range []string{"area@v1", "area@v2"} {
		if _, err := os.Stat(filepath.Join(r.Root, ".kton", "packages", d)); err != nil {
			t.Fatalf("%s is not installed: %v", d, err)
		}
	}
	for _, tm := range []string{"decides", "records"} {
		if err := say(tm); err != nil {
			t.Errorf("say %s with area@v1 and area@v2 installed: %v", tm, err)
		}
	}
}
