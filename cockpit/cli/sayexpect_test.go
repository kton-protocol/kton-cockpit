package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// say --expect-template and --expect-revision hold a consent to the template a person was shown: when
// v2 of the package changes the template behind the same name, say refuses template.changed instead of
// signing the new predicate (kton-review R1 on #177; userinterface's request on #177 (c)).
func TestSay_ExpectTemplateAndRevisionRefuseATemplateThatChanged(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	dir := filepath.Join(t.TempDir(), "qm")
	buildPkg(t, dir, "qm", "v1", key, map[string]string{"approves": tmpl("approves", "https://example.org/approves-for-release")})

	listed := func() (rev, hash string) {
		t.Helper()
		stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
		var l struct {
			Packages []struct {
				Name     string     `json:"name"`
				Revision string     `json:"revision"`
				Members  []UIMember `json:"members"`
			} `json:"packages"`
		}
		if json.Unmarshal([]byte(stdout), &l) != nil {
			t.Fatalf("ui list --json: %s", stdout)
		}
		for _, p := range l.Packages {
			for _, m := range p.Members {
				if p.Members != nil && m.Name == "approves" {
					rev, hash = p.Revision, m.Hash
				}
			}
		}
		if hash == "" {
			t.Fatalf("no usable approves template listed: %s", stdout)
		}
		return rev, hash
	}
	rev1, hash1 := listed()
	if out, code := r.Cockpit(t, "say", "approves", "urn:example:one", "note=ok", "--expect-template", hash1, "--expect-revision", rev1); code != 0 {
		t.Fatalf("say with the template and revision shown (exit %d):\n%s", code, out)
	}

	// qm v2: the same name now means another predicate.
	p, err := scope.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := p.WithSigner(key, scope.Options{}).NewRevision("v2")
	if err != nil {
		t.Fatal(err)
	}
	if err := rev.SetProfile(scope.ProfileTemplates); err != nil {
		t.Fatal(err)
	}
	if _, err := rev.AddTemplate("approves", []byte(tmpl("approves", "https://example.org/rejects-release"))); err != nil {
		t.Fatal(err)
	}
	if _, err := rev.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatal(err)
	}

	for _, flags := range [][]string{{"--expect-template", hash1}, {"--expect-revision", rev1}} {
		args := append([]string{"say", "approves", "urn:example:two", "note=ok"}, flags...)
		if out, code := r.Cockpit(t, args...); code != 2 || !strings.Contains(out, "read it again, then say again") {
			t.Errorf("say %s after v2 changed the template (exit %d):\n%s", flags[0], code, out)
		}
	}
	rev2, hash2 := listed()
	if out, code := r.Cockpit(t, "say", "approves", "urn:example:two", "note=ok", "--expect-template", hash2, "--expect-revision", rev2); code != 0 {
		t.Errorf("say with v2's template and revision (exit %d):\n%s", code, out)
	}
	if out, code := r.Cockpit(t, "say", "approves", "urn:example:two", "note=ok", "--expect-template", "nonsense"); code != 2 || !strings.Contains(out, "content hash") {
		t.Errorf("a malformed expectTemplate (exit %d):\n%s", code, out)
	}
}
