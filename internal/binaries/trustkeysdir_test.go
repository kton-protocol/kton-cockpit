package binaries

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
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

func TestTrustKeysDir_SamePubkeyInTwoTiersMaterializesOnce(t *testing.T) {
	root := t.TempDir()
	writeKey(t, filepath.Join(root, "shared.pub"), "shared-hex")

	// TierPubkeys keys its map by resolved pubkey path, so the same path listed under two tiers
	// collapses to one entry — trustKeysDir must not double-write it.
	cfg := testConfig(root, map[string][]string{
		"self": {"shared.pub"},
		"team": {"shared.pub"},
	})

	dir, cleanup, err := trustKeysDir(cfg, "")
	if err != nil {
		t.Fatalf("trustKeysDir: %v", err)
	}
	defer cleanup()

	got := readPubDir(t, dir)
	if len(got) != 1 {
		t.Fatalf("expected the shared pubkey to materialize exactly once, got %d entries: %v", len(got), got)
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

func TestTrustKeysDir_MissingPubkeyPathFailsAndCleansUp(t *testing.T) {
	cfg := testConfig(t.TempDir(), map[string][]string{
		"self": {"does-not-exist.pub"},
	})

	dir, _, err := trustKeysDir(cfg, "")
	if err == nil {
		t.Fatal("expected an error for a configured pubkey path that does not exist on disk")
	}
	if dir != "" {
		t.Fatalf("expected no usable directory on error, got %q", dir)
	}
}

func testConfig(repoRoot string, tiers map[string][]string) *config.Config {
	return &config.Config{
		RepoRoot: repoRoot,
		Raw: config.Raw{
			Trust: config.Trust{Tiers: tiers},
		},
	}
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
