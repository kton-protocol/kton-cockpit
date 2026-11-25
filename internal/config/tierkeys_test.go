package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A tier's keys are the .pub files in its folder, whatever characters the repository's path holds:
// the path is not a pattern (kton-review G1 on #254). With Glob, a repository at .../repo* trusted
// .../repoEVIL's peers key, and one at .../re\po trusted nothing.
func TestConfig_ATiersKeysAreItsFolderNotAPattern(t *testing.T) {
	base := t.TempDir()
	put := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(base, "repoEVIL", "keys", "peers", "attacker.pub"))
	for _, name := range []string{"repo*", `re\po`, "re[p]o", "rep?"} {
		keys := filepath.Join(base, name, "keys")
		own := filepath.Join(keys, "peers", "alice.pub")
		put(own)
		put(filepath.Join(keys, "peers", "notes.txt"))
		c := &Config{KeysDir: keys}
		got := c.TierKeys("peers")
		if len(got) != 1 || got[0] != own {
			t.Errorf("repository %q: peers keys %v, want only %s", name, got, own)
		}
	}
	if got := (&Config{KeysDir: filepath.Join(base, "none", "keys")}).TierKeys("peers"); len(got) != 0 {
		t.Errorf("a tier without a folder: %v, want none", got)
	}
}
