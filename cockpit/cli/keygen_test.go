package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A flag where the name goes is a flag someone tried, not a name. `keygen --help` used to write
// keys/--help.key and keys/--help-claims.key and say nothing was wrong.
func TestKeygen_RefusesAFlagAsName(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, arg := range []string{"--help", "-h", "-x"} {
		if err := runKeygen(context.Background(), []string{arg}); err == nil {
			t.Errorf("keygen %s: accepted", arg)
		}
	}
	if _, err := os.Stat("keys"); !os.IsNotExist(err) {
		entries, _ := os.ReadDir("keys")
		t.Errorf("a refused keygen wrote keys/: %v", entries)
	}
}

// The negative control: a plain name still makes both halves of the identity.
func TestKeygen_MakesBothHalves(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runKeygen(context.Background(), []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"alice.key", "alice.pub", "alice-claims.key", "alice-claims.pub"} {
		if _, err := os.Stat(filepath.Join("keys", f)); err != nil {
			t.Errorf("keys/%s: %v", f, err)
		}
	}
}

// After init named a placeholder, keygen binds the identity it made: init → keygen needs no edit.
// The negative control is the second keygen: an identity that exists is not replaced.
func TestKeygen_BindsAPlaceholderIdentityOnly(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	cfgPath := filepath.Join(r.Root, "cockpit.config.json")
	b, _ := os.ReadFile(cfgPath)
	must(t, os.WriteFile(cfgPath, []byte(strings.ReplaceAll(string(b), testrepo.SessionID, "placeholder")), 0o644))

	must(t, runKeygen(context.Background(), []string{"alice"}))
	b, _ = os.ReadFile(cfgPath)
	if !strings.Contains(string(b), `"plankton_key": "keys/alice.key"`) || !strings.Contains(string(b), `"registry/keys/alice-claims.pub"`) {
		t.Fatalf("placeholder not replaced:\n%s", b)
	}

	must(t, runKeygen(context.Background(), []string{"bob"}))
	b, _ = os.ReadFile(cfgPath)
	if strings.Contains(string(b), "keys/bob.key") {
		t.Errorf("an existing identity was replaced by bob:\n%s", b)
	}
}

// A configuration that is there and does not load is said, not taken for "none yet": the keys would
// otherwise land unbound and the reason unsaid.
func TestKeygen_ABrokenConfigIsSaid(t *testing.T) {
	t.Chdir(t.TempDir())
	must(t, os.WriteFile("cockpit.config.json", []byte("{not json"), 0o644))
	if err := runKeygen(context.Background(), []string{"alice"}); err == nil {
		t.Fatal("keygen went ahead past a configuration that does not load")
	}
	if _, err := os.Stat("keys"); !os.IsNotExist(err) {
		t.Error("keys were written anyway")
	}
}
