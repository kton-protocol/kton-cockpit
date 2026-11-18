package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The identity names keys outside the repository. One that names a key inside it is refused when
// the configuration loads, so nothing signs with it (ADR-016, kton-review L3). No identity file is
// not an error: a fresh clone can still ask.
func TestIdentity_AKeyInsideTheRepositoryIsRefused(t *testing.T) {
	root := t.TempDir()
	write := func(json string) {
		t.Helper()
		os.MkdirAll(filepath.Join(root, Dir, "local"), 0o755)
		if err := os.WriteFile(IdentityPath(root), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if id, err := loadIdentity(root); err != nil || id.SessionID != "" {
		t.Fatalf("no identity file: want none and no error, got %+v %v", id, err)
	}
	inside := filepath.Join(root, "keys", "a.key")
	write(`{"session_id":"a","plankton_key":"` + inside + `","nekton_key":"/elsewhere/a-claims.key"}`)
	if _, err := loadIdentity(root); err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Errorf("a key inside the repository: %v", err)
	}
	write(`{"session_id":"a","plankton_key":"keys/a.key","nekton_key":"/elsewhere/a-claims.key"}`)
	if _, err := loadIdentity(root); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("a relative key path: %v", err)
	}
	write(`{"session_id":"a","plankton_key":"/elsewhere/a.key","nekton_key":"~/.kton-keys/a-claims.key"}`)
	if id, err := loadIdentity(root); err != nil || id.PlanktonKey != "/elsewhere/a.key" {
		t.Errorf("keys outside: %+v %v", id, err)
	}
}

// The names under .kton/ that are the cockpit's are listed, so another tool keeps to its own folder
// (ADR-016, kton-review L4), and every folder of the layout is one of them.
func TestLayout_TheReservedNamesCoverTheLayout(t *testing.T) {
	c := Layout("/r", Raw{})
	for _, p := range []string{c.PlanktonDir, c.NektonDir, c.TemplatesDir, c.KeysDir, c.BlobsDir, c.PackagesDir, c.LocalDir} {
		if !slices.Contains(Reserved, filepath.Base(p)) || filepath.Dir(p) != c.KtonDir {
			t.Errorf("%s is not a reserved name directly under .kton/", p)
		}
	}
	if !slices.Contains(Reserved, ConfigFile) {
		t.Error("the configuration file is not reserved")
	}
	if c.RunsDir != filepath.Join("/r", "work") {
		t.Errorf("runs default to %s, want /r/work", c.RunsDir)
	}
}
