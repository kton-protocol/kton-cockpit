package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
