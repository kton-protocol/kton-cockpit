package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The templates init writes are the participant skeleton's, byte for byte. Two copies exist because
// go:embed cannot reach outside this package; this keeps them one.
func TestInitTemplates_MatchTheSkeleton(t *testing.T) {
	ents, err := skeletonTemplates.ReadDir("skeleton/templates")
	if err != nil || len(ents) == 0 {
		t.Fatalf("no embedded templates: %v", err)
	}
	for _, e := range ents {
		got, _ := skeletonTemplates.ReadFile("skeleton/templates/" + e.Name())
		want, err := os.ReadFile(filepath.Join("..", "..", "uat", "participant-skeleton", "templates", e.Name()))
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from uat/participant-skeleton/templates", e.Name())
		}
	}
}

func scaffoldRaw() config.Raw {
	return config.Raw{
		Paths:    config.Paths{TemplatesDir: "templates", KeysDir: "keys"},
		Identity: config.Identity{SessionID: "session-1", PlanktonKey: "keys/session-1.key", NektonKey: "keys/session-1-claims.key"},
		Claims:   config.Claims{AllowedTemplates: []string{"reproduces", "working-on", "not-carried"}},
		Trust:    config.Trust{Tiers: map[string][]string{"self": {}}},
	}
}

// After init the allowed templates exist and the one identity under keys/ is the one that signs, so
// say works without a hand edit. An existing template is the operator's and stays as it is.
func TestInitScaffold_TemplatesAndTheOneIdentity(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "keys"), 0o700))
	for _, f := range []string{"alice.key", "alice.pub", "alice-claims.key", "alice-claims.pub"} {
		must(t, os.WriteFile(filepath.Join(root, "keys", f), []byte("x"), 0o600))
	}
	must(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "templates", "working-on.json"), []byte("mine"), 0o644))

	raw := scaffoldRaw()
	if _, err := scaffold(root, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "templates", "reproduces.json")); err != nil {
		t.Errorf("reproduces.json not written: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "templates", "working-on.json")); string(b) != "mine" {
		t.Errorf("an existing template was overwritten: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "templates", "not-carried.json")); !os.IsNotExist(err) {
		t.Errorf("a template this binary does not carry was invented")
	}
	if raw.Identity.PlanktonKey != "keys/alice.key" || raw.Identity.NektonKey != "keys/alice-claims.key" {
		t.Errorf("identity not bound: %+v", raw.Identity)
	}
	// Both halves, or the repository's own claims do not verify here.
	if got := raw.Trust.Tiers["self"]; len(got) != 2 || got[0] != "keys/alice.pub" || got[1] != "keys/alice-claims.pub" {
		t.Errorf("tier self: %v", got)
	}
}

// The negative control: with two identities there is no telling which one signs, so none is chosen.
func TestInitScaffold_TwoIdentitiesChooseNone(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "keys"), 0o700))
	for _, n := range []string{"alice", "bob"} {
		for _, s := range []string{".key", "-claims.key"} {
			must(t, os.WriteFile(filepath.Join(root, "keys", n+s), []byte("x"), 0o600))
		}
	}
	raw := scaffoldRaw()
	if _, err := scaffold(root, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Identity.SessionID != "session-1" || len(raw.Trust.Tiers["self"]) != 0 {
		t.Errorf("an identity was chosen among several: %+v %v", raw.Identity, raw.Trust.Tiers)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
