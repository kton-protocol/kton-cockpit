package cli

import (
	"bytes"
	"encoding/json"
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
	gi, _ := skeletonTemplates.ReadFile("skeleton/gitignore")
	if want, _ := os.ReadFile(filepath.Join("..", "..", "uat", "participant-skeleton", "gitignore")); !bytes.Equal(gi, want) {
		t.Errorf("gitignore differs from uat/participant-skeleton/gitignore")
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
		Claims: config.Claims{AllowedTemplates: []string{"reproduces", "working-on", "not-carried"}},
		Trust:  config.Trust{Tiers: []string{"self"}},
	}
}

// After init the .kton/ tree and the allowed templates exist, and the one identity in the machine's
// key folder is the one that signs, so say works without a hand edit. The private keys stay where
// they are, outside the repository (ADR-016). An existing template is the operator's and stays.
func TestInitScaffold_TemplatesAndTheOneIdentity(t *testing.T) {
	root, ring := t.TempDir(), t.TempDir()
	t.Setenv("KTON_KEYS", ring)
	for _, f := range []string{"alice.key", "alice.pub", "alice-claims.key", "alice-claims.pub"} {
		must(t, os.WriteFile(filepath.Join(ring, f), []byte(f), 0o600))
	}
	tdir := filepath.Join(root, ".kton", "templates")
	must(t, os.MkdirAll(tdir, 0o755))
	must(t, os.WriteFile(filepath.Join(tdir, "working-on.json"), []byte("mine"), 0o644))

	raw := scaffoldRaw()
	if _, err := scaffold(root, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tdir, "reproduces.json")); err != nil {
		t.Errorf("reproduces.json not written: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(tdir, "working-on.json")); string(b) != "mine" {
		t.Errorf("an existing template was overwritten: %q", b)
	}
	if _, err := os.Stat(filepath.Join(tdir, "not-carried.json")); !os.IsNotExist(err) {
		t.Errorf("a template this binary does not carry was invented")
	}
	// Both halves, or the repository's own claims do not verify here.
	for _, f := range []string{"alice.pub", "alice-claims.pub"} {
		if b, err := os.ReadFile(filepath.Join(root, ".kton", "keys", "self", f)); err != nil || string(b) != f {
			t.Errorf("public key %s not in tier self: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".kton", ".gitignore")); err != nil {
		t.Errorf(".kton/.gitignore not written: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, ".kton", "*", "*.key"))
	deeper, _ := filepath.Glob(filepath.Join(root, ".kton", "*", "*", "*.key"))
	if len(matches)+len(deeper) != 0 {
		t.Errorf("a private key was copied into the repository: %v %v", matches, deeper)
	}
	b, err := os.ReadFile(config.IdentityPath(root))
	if err != nil {
		t.Fatalf("identity not bound: %v", err)
	}
	var id config.Identity
	must(t, json.Unmarshal(b, &id))
	if id.SessionID != "alice" || id.PlanktonKey != filepath.Join(ring, "alice.key") || id.NektonKey != filepath.Join(ring, "alice-claims.key") {
		t.Errorf("identity: %+v", id)
	}
}

// The negative control: with two identities there is no telling which one signs, so none is chosen.
func TestInitScaffold_TwoIdentitiesChooseNone(t *testing.T) {
	root, ring := t.TempDir(), t.TempDir()
	t.Setenv("KTON_KEYS", ring)
	for _, n := range []string{"alice", "bob"} {
		for _, s := range []string{".key", ".pub", "-claims.key", "-claims.pub"} {
			must(t, os.WriteFile(filepath.Join(ring, n+s), []byte("x"), 0o600))
		}
	}
	raw := scaffoldRaw()
	if _, err := scaffold(root, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.IdentityPath(root)); !os.IsNotExist(err) {
		t.Errorf("an identity was chosen among several: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(root, ".kton", "keys", "self", "*.pub")); len(m) != 0 {
		t.Errorf("keys were trusted without an identity: %v", m)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
