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
		return sayListed(t, r, "approves")
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

// say --json lists a template's predicate as say will sign it, resolved through the alias file, with
// the hash of the template as signed; when aliases.json changes what a prefix means, the listing
// follows, and both --expect-predicate and --expect-template refuse though the template file is
// unchanged (kton-review R1b on #177, E1 on #178).
func TestSay_TheListingGivesThePredicateAsSigned(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, ".kton/templates/release.json", `{"name": "release", "target": "either", "predicate": "rel:approves", "fields": {"note": {"type": "string"}}}`)
	r.Write(t, ".kton/aliases.json", `{"prefixes": {"rel": "https://example.org/release/"}}`)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "release")
	r.WriteConfig(t, raw)

	listed := func() Template {
		t.Helper()
		stdout, _, _ := r.CockpitSplit(t, "say", "--json")
		var ts []Template
		if json.Unmarshal([]byte(stdout), &ts) != nil {
			t.Fatalf("say --json: %s", stdout)
		}
		for _, x := range ts {
			if x.Name == "release" {
				return x
			}
		}
		t.Fatalf("release is not listed: %s", stdout)
		return Template{}
	}
	before := listed()
	if before.Predicate != "https://example.org/release/approves" || !strings.HasPrefix(before.Hash, "sha256:") {
		t.Fatalf("the listing does not give the predicate as signed, with the hash: %+v", before)
	}
	r.Write(t, ".kton/aliases.json", `{"prefixes": {"rel": "https://example.org/reject/"}}`)
	after := listed()
	if after.Predicate != "https://example.org/reject/approves" || after.Hash == before.Hash {
		t.Errorf("after the alias change the listing does not follow, or the hash stays: %+v (before %+v)", after, before)
	}
	if out, code := r.Cockpit(t, "say", "release", "urn:example:x", "note=ok", "--expect-template", before.Hash); code != 2 || !strings.Contains(out, "read it again, then say again") {
		t.Errorf("say with the hash listed before the alias change (exit %d):\n%s", code, out)
	}
	if out, code := r.Cockpit(t, "say", "release", "urn:example:x", "note=ok", "--expect-predicate", before.Predicate); code != 2 || !strings.Contains(out, "read it again, then say again") {
		t.Errorf("say with the predicate listed before the alias change (exit %d):\n%s", code, out)
	}
	if out, code := r.Cockpit(t, "say", "release", "urn:example:x", "note=ok", "--expect-predicate", after.Predicate, "--expect-template", after.Hash); code != 0 {
		t.Errorf("say with the predicate and hash listed now (exit %d):\n%s", code, out)
	}
}

// sayListed is the hash and revision say --json lists for a template.
func sayListed(t *testing.T, r *testrepo.Repo, name string) (rev, hash string) {
	t.Helper()
	stdout, _, _ := r.CockpitSplit(t, "say", "--json")
	var ts []Template
	if json.Unmarshal([]byte(stdout), &ts) != nil {
		t.Fatalf("say --json: %s", stdout)
	}
	for _, x := range ts {
		if x.Name == name && x.Hash != "" {
			return x.Revision, x.Hash
		}
	}
	t.Fatalf("%s is not listed with a hash: %s", name, stdout)
	return "", ""
}

// A template two packages bring, offered as <package>/<name>, takes the hash and revision say --json
// lists for it (kton-review E2 on #178).
func TestSay_ExpectTemplateHoldsForAQualifiedTemplate(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	for _, n := range []string{"pa", "pb"} {
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		buildPkg(t, filepath.Join(t.TempDir(), n), n, "v1", key, map[string]string{"approves": tmpl("approves", "https://example.org/"+n+"/approves")})
	}
	rev, hash := sayListed(t, r, "pa/approves")
	if rev == "" {
		t.Fatal("a package template is listed with no revision")
	}
	if out, code := r.Cockpit(t, "say", "pa/approves", "urn:example:x", "note=ok", "--expect-template", hash, "--expect-revision", rev); code != 0 {
		t.Errorf("say pa/approves with what say --json lists (exit %d):\n%s", code, out)
	}
	_, other := sayListed(t, r, "pb/approves")
	if out, code := r.Cockpit(t, "say", "pa/approves", "urn:example:x", "note=ok", "--expect-template", other); code != 2 {
		t.Errorf("say pa/approves with pb's hash (exit %d):\n%s", code, out)
	}
}
