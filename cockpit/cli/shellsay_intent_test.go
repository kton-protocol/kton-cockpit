package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// say --json and say/templates carry a template's declared intent, signerRoles and scopes (kton #301,
// architecture view G9), as the template states them, and leave both keys out when it states none
// (SPEC §8.5).
func TestShellSay_ListsATemplatesSignerRolesAndScopesOnlyWhenDeclared(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const role = "https://example.org/roles/reviewer"
	seed := "https://kton.dev/o/" + strings.Repeat("ab", 32)
	src := filepath.Join(t.TempDir(), "templates")
	must(os.MkdirAll(src, 0o755))
	must(os.WriteFile(filepath.Join(src, "reviews.json"), []byte(`{"name": "reviews", "target": "either",
  "predicate": "https://example.org/ln/reviews", "signerRoles": ["`+role+`"], "scopes": ["`+seed+`"],
  "fields": {"verdict": {"type": "string", "required": true}}}`), 0o644))
	must(os.WriteFile(filepath.Join(src, "notes.json"), []byte(`{"name": "notes", "target": "either",
  "predicate": "https://example.org/ln/notes", "fields": {"text": {"type": "string", "required": true}}}`), 0o644))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	pkg := filepath.Join(t.TempDir(), "intent")
	_, _, err = scope.BuildTemplates(pkg, "intent", "v1", src, "", key, scope.Options{})
	must(err)
	_, err = cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true})
	must(err)

	cfg, err := config.Load(context.Background(), ".")
	must(err)
	listed, err := sayListing(cfg)
	must(err)
	seen := map[string]bool{}
	for _, tmpl := range listed {
		raw, err := json.Marshal(tmpl)
		must(err)
		var keys map[string]json.RawMessage
		must(json.Unmarshal(raw, &keys))
		switch tmpl.Name {
		case "reviews":
			seen["reviews"] = true
			if !reflect.DeepEqual(tmpl.SignerRoles, []string{role}) || !reflect.DeepEqual(tmpl.Scopes, []string{seed}) {
				t.Errorf("reviews: signerRoles %v scopes %v, want [%s] [%s]", tmpl.SignerRoles, tmpl.Scopes, role, seed)
			}
			if keys["signerRoles"] == nil || keys["scopes"] == nil {
				t.Errorf("reviews: JSON lacks signerRoles or scopes: %s", raw)
			}
		case "notes":
			seen["notes"] = true
			if _, ok := keys["signerRoles"]; ok {
				t.Errorf("notes declares no signer roles, JSON has the key: %s", raw)
			}
			if _, ok := keys["scopes"]; ok {
				t.Errorf("notes declares no scopes, JSON has the key: %s", raw)
			}
		}
	}
	if !seen["reviews"] || !seen["notes"] {
		t.Fatalf("package templates not listed: %v", seen)
	}
}

// A role written as a CURIE is listed as the IRI it resolves to: through the package's own alias file for
// a package template, which nekton otherwise refuses as unresolvable and say drops (kton-review F1 on
// #260), and through .kton/aliases.json for a repository template (F2).
func TestShellSay_ListsCURIERolesResolved(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(t.TempDir(), "templates")
	must(os.MkdirAll(src, 0o755))
	must(os.WriteFile(filepath.Join(src, "aliases.json"), []byte(`{"prefixes": {"pr": "https://example.org/pkgroles/"}}`), 0o644))
	must(os.WriteFile(filepath.Join(src, "approves.json"), []byte(`{"name": "approves", "target": "either",
  "predicate": "https://example.org/ln/approves", "signerRoles": ["pr:approver"],
  "fields": {"note": {"type": "string"}}}`), 0o644))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	pkg := filepath.Join(t.TempDir(), "roles")
	_, _, err = scope.BuildTemplates(pkg, "roles", "v1", src, "", key, scope.Options{})
	must(err)
	_, err = cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true})
	must(err)

	r.Write(t, ".kton/templates/signs.json", `{"name": "signs", "target": "either", "predicate": "https://example.org/ln/signs",
  "signerRoles": ["rr:signer"], "fields": {"note": {"type": "string"}}}`)
	r.Write(t, ".kton/aliases.json", `{"prefixes": {"rr": "https://example.org/reporoles/"}}`)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "signs")
	r.WriteConfig(t, raw)

	cfg, err := config.Load(context.Background(), ".")
	must(err)
	listed, err := sayListing(cfg)
	must(err)
	want := map[string]string{"approves": "https://example.org/pkgroles/approver", "signs": "https://example.org/reporoles/signer"}
	got := map[string][]string{}
	for _, tmpl := range listed {
		if _, ok := want[tmpl.Name]; ok {
			got[tmpl.Name] = tmpl.SignerRoles
		}
	}
	for name, role := range want {
		if roles, ok := got[name]; !ok {
			t.Errorf("%s is not listed (refused?): %v", name, got)
		} else if !reflect.DeepEqual(roles, []string{role}) {
			t.Errorf("%s: signerRoles %v, want [%s]", name, roles, role)
		}
	}
}
