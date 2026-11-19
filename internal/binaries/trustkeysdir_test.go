package binaries

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// These exercise trustKeysDir's own materialization logic directly — no plankton binary involved
// — since Reproductions' one live test can't reach this code path (the vendored binary rejects
// --trust-keys before ever reading the directory it names).

func TestTrustKeysDir_MaterializesEveryConfiguredPubkey(t *testing.T) {
	root := t.TempDir()
	writeKey(t, filepath.Join(root, "a.pub"), "keyA-hex")
	writeKey(t, filepath.Join(root, "b.pub"), "keyB-hex")

	cfg := testConfig(root, map[string][]string{
		"self": {"a.pub"},
		"team": {"b.pub"},
	})

	dir, cleanup, err := trustKeysDir(cfg, "")
	if err != nil {
		t.Fatalf("trustKeysDir: %v", err)
	}
	defer cleanup()

	got := readPubDir(t, dir)
	want := map[string]bool{"keyA-hex": true, "keyB-hex": true}
	if len(got) != 2 {
		t.Fatalf("expected 2 materialized keys, got %d: %v", len(got), got)
	}
	for _, c := range got {
		if !want[c] {
			t.Fatalf("unexpected materialized key content %q", c)
		}
	}
}

func TestTrustKeysDir_EmptyTrustTiersMaterializesEmptyDir(t *testing.T) {
	cfg := testConfig(t.TempDir(), map[string][]string{})

	dir, cleanup, err := trustKeysDir(cfg, "")
	if err != nil {
		t.Fatalf("trustKeysDir: %v", err)
	}
	defer cleanup()

	if got := readPubDir(t, dir); len(got) != 0 {
		t.Fatalf("expected no materialized keys for an empty trust config, got %v", got)
	}
}

// A tier is its folder's *.pub files: anything else there is not a key and is not handed on.
func TestTrustKeysDir_OnlyThePubFilesOfATierFolder(t *testing.T) {
	root := t.TempDir()
	writeKey(t, filepath.Join(root, "a.pub"), "keyA-hex")
	cfg := testConfig(root, map[string][]string{"self": {"a.pub"}})
	writeKey(t, filepath.Join(cfg.TierDir("self"), "README.md"), "not a key")

	dir, cleanup, err := trustKeysDir(cfg, "")
	if err != nil {
		t.Fatalf("trustKeysDir: %v", err)
	}
	defer cleanup()
	if got := readPubDir(t, dir); len(got) != 1 || got[0] != "keyA-hex" {
		t.Fatalf("want only a.pub, got %v", got)
	}
}

// testConfig is a configuration with the given tiers, each holding copies of the named files
// from repoRoot in its folder .kton/keys/<tier>/.
func testConfig(repoRoot string, tiers map[string][]string) *config.Config {
	names := make([]string, 0, len(tiers))
	for tier := range tiers {
		names = append(names, tier)
	}
	cfg := config.Layout(repoRoot, config.Raw{Trust: config.Trust{Tiers: names}})
	for tier, files := range tiers {
		os.MkdirAll(cfg.TierDir(tier), 0o755)
		for _, f := range files {
			if b, err := os.ReadFile(filepath.Join(repoRoot, f)); err == nil {
				os.WriteFile(filepath.Join(cfg.TierDir(tier), f), b, 0o644)
			}
		}
	}
	return cfg
}

func writeKey(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPubDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}
