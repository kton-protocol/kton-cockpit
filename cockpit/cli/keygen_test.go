package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A flag where the name goes is a flag someone tried, not a name. `keygen --help` used to write
// keys/--help.key and keys/--help-claims.key and say nothing was wrong.
func TestKeygen_RefusesAFlagAsName(t *testing.T) {
	t.Chdir(t.TempDir())
	ring := filepath.Join(t.TempDir(), "ring")
	t.Setenv("KTON_KEYS", ring)
	for _, arg := range []string{"--help", "-h", "-x"} {
		if err := runKeygen(context.Background(), []string{arg}); err == nil {
			t.Errorf("keygen %s: accepted", arg)
		}
	}
	if _, err := os.Stat(ring); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(ring)
		t.Errorf("a refused keygen wrote keys: %v", entries)
	}
}

// The negative control: a plain name still makes both halves of the identity, in the machine's key
// folder and nowhere in the current directory (ADR-016).
func TestKeygen_MakesBothHalves(t *testing.T) {
	t.Chdir(t.TempDir())
	ring := t.TempDir()
	t.Setenv("KTON_KEYS", ring)
	if err := runKeygen(context.Background(), []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"alice.key", "alice.pub", "alice-claims.key", "alice-claims.pub"} {
		if _, err := os.Stat(filepath.Join(ring, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if ents, _ := os.ReadDir("."); len(ents) != 0 {
		t.Errorf("keygen wrote into the current directory: %v", ents)
	}
}

// In a checkout without an identity, keygen binds the one it made: init → keygen needs no edit. The
// negative control is the second keygen: an identity that exists is not replaced, though the new
// public keys are put into tier self.
func TestKeygen_BindsOnlyWhenTheCheckoutHasNoIdentity(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	ring := t.TempDir()
	t.Setenv("KTON_KEYS", ring)
	must(t, os.Remove(config.IdentityPath(r.Root)))

	must(t, runKeygen(context.Background(), []string{"alice"}))
	var id config.Identity
	b, _ := os.ReadFile(config.IdentityPath(r.Root))
	must(t, json.Unmarshal(b, &id))
	if id.SessionID != "alice" || id.PlanktonKey != filepath.Join(ring, "alice.key") {
		t.Fatalf("identity not bound: %s", b)
	}
	if _, err := os.Stat(filepath.Join(r.Root, ".kton", "keys", "self", "alice-claims.pub")); err != nil {
		t.Errorf("alice's claim key not in tier self: %v", err)
	}

	must(t, runKeygen(context.Background(), []string{"bob"}))
	b, _ = os.ReadFile(config.IdentityPath(r.Root))
	must(t, json.Unmarshal(b, &id))
	if id.SessionID != "alice" {
		t.Errorf("an existing identity was replaced by bob: %s", b)
	}
}

// A key folder inside the repository is refused: a private key there is one commit from being
// shared (ADR-016, kton-review L3).
func TestKeygen_RefusesAKeyFolderInsideTheRepository(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	t.Setenv("KTON_KEYS", filepath.Join(r.Root, "keys"))
	if err := runKeygen(context.Background(), []string{"alice"}); err == nil {
		t.Fatal("keygen wrote private keys into the repository")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "keys")); !os.IsNotExist(err) {
		t.Error("keys were written anyway")
	}
}

// A configuration that is there and does not load is said, not taken for "none yet": the keys would
// otherwise land unbound and the reason unsaid.
func TestKeygen_ABrokenConfigIsSaid(t *testing.T) {
	t.Chdir(t.TempDir())
	ring := filepath.Join(t.TempDir(), "ring")
	t.Setenv("KTON_KEYS", ring)
	must(t, os.MkdirAll(".kton", 0o755))
	must(t, os.WriteFile(filepath.Join(".kton", "cockpit.config.json"), []byte("{not json"), 0o644))
	if err := runKeygen(context.Background(), []string{"alice"}); err == nil {
		t.Fatal("keygen went ahead past a configuration that does not load")
	}
	if _, err := os.Stat(ring); !os.IsNotExist(err) {
		t.Error("keys were written anyway")
	}
}
