package tools

import (
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func denylistTestConfig() *config.Config {
	repoRoot := "/repo/root"
	return &config.Config{
		RepoRoot: repoRoot,
		KeysDir:  filepath.Join(repoRoot, "keys"), // what config.Load actually resolves KeysDir to
		Raw: config.Raw{
			Paths: config.Paths{KeysDir: "keys"},
		},
	}
}

func TestValidatePublishPath_AllowsOrdinaryRepoRelativePaths(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{
		"data/penguins.csv",
		"session-1/clean.py",
		"out.csv",
		"corpus/session-1-12345.json",
	} {
		if err := validatePublishPath(cfg, p); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", p, err)
		}
	}
}

func TestValidatePublishPath_RejectsEmptyPath(t *testing.T) {
	cfg := denylistTestConfig()
	if err := validatePublishPath(cfg, ""); err == nil {
		t.Fatal("expected an empty path to be rejected")
	}
}

func TestValidatePublishPath_RejectsAbsolutePaths(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{"/etc/passwd", "/repo/root/data/penguins.csv"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected absolute path %q to be rejected", p)
		}
	}
}

func TestValidatePublishPath_RejectsDotDotTraversal(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{
		"..",
		"../keys/session-1.key",
		"a/../../etc/passwd",
		"data/../../outside.txt",
	} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected traversal path %q to be rejected", p)
		}
	}
}

// TestValidatePublishPath_RejectsLeadingDashPaths is a regression test for the critical bypass an
// independent cold-session review found: gitops.CommitAndPush built `git add` with no "--"
// separator, so a path like "-f" was parsed by git as a FLAG, not a literal path -
// outputs: ["-f", "."] became `git add -f .`, force-adding every gitignored file including real
// keys, without the string "keys/..." ever appearing in the request - strictly worse than the
// hole Michael originally described. Fixed at the root cause in gitops.go (a "--" separator) AND
// here, as defense in depth, in case some future refactor ever drops that separator or feeds git
// a path that was never validated here in the first place.
func TestValidatePublishPath_RejectsLeadingDashPaths(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{"-f", "--force", "-", "-rf", "-x/data.csv"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected leading-dash path %q to be rejected as a potential git flag", p)
		}
	}
}

func TestValidatePublishPath_RejectsKeysDirAndNestedFiles(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{"keys", "keys/session-1.key", "keys/nested/dir/file.txt", "KEYS/session-1.key"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected path under keys_dir %q to be rejected", p)
		}
	}
}

// TestValidatePublishPath_AnchoredKeysDirSpellingStillDetected is a regression test for a bug an
// independent cold-session review found: comparing against the RAW cfg.Raw.Paths.KeysDir string
// (rather than the already-resolved absolute cfg.KeysDir) would silently stop matching anything if
// an operator ever wrote keys_dir anchored like "/keys" - config.validate() doesn't forbid that
// spelling, and config.Load still resolves cfg.KeysDir correctly regardless, but the raw string
// itself stays "/keys", which never shares a prefix with a repo-relative cleaned path.
func TestValidatePublishPath_AnchoredKeysDirSpellingStillDetected(t *testing.T) {
	repoRoot := "/repo/root"
	cfg := &config.Config{
		RepoRoot: repoRoot,
		KeysDir:  filepath.Join(repoRoot, "keys"), // what config.Load actually produces
		Raw: config.Raw{
			Paths: config.Paths{KeysDir: "/keys"}, // anchored spelling in the raw config file
		},
	}
	if err := validatePublishPath(cfg, "keys/session-1.key"); err == nil {
		t.Fatal("expected a key under an anchored keys_dir spelling to still be rejected")
	}
}

func TestValidatePublishPath_DoesNotFalsePositiveOnSimilarlyNamedDir(t *testing.T) {
	// "keys2" is a real, different directory - must not collide with the configured "keys" dir via
	// a naive string-prefix check.
	cfg := denylistTestConfig()
	if err := validatePublishPath(cfg, "keys2/file.txt"); err != nil {
		t.Fatalf("expected keys2/file.txt (NOT the configured keys_dir) to be allowed, got: %v", err)
	}
}

func TestValidatePublishPath_RejectsGitInternals(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{".git", ".git/config", ".git/hooks/pre-commit", ".GIT/config"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected .git-internal path %q to be rejected", p)
		}
	}
}

func TestValidatePublishPath_RejectsOwnConfigFile(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{"cockpit.config.json", "Cockpit.Config.JSON"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected %q (this repo's own config) to be rejected", p)
		}
	}
}

func TestValidatePublishPath_RejectsDotKeyExtensionAnywhere(t *testing.T) {
	cfg := denylistTestConfig()
	// Not under keys_dir at all - the *.key check must be a floor of protection independent of
	// keys_dir being configured correctly.
	for _, p := range []string{"session-1.key", "some/other/dir/rogue.KEY", "outputs/oops.key"} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected *.key path %q to be rejected regardless of location", p)
		}
	}
}

// TestValidatePublishPath_RejectsKeyExtensionWithTrailingDotOrSpace is a regression test for a
// bypass an independent cold-session review found: filepath.Ext("session-1.key.") returns ".",
// not ".key", so a trailing dot or space let a key slip past the *.key check entirely - directly
// contradicting that check's own "regardless of location" claim.
func TestValidatePublishPath_RejectsKeyExtensionWithTrailingDotOrSpace(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{"session-1.key.", "session-1.key ", "outputs/rogue.key.."} {
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected %q (a .key hidden behind a trailing dot/space) to be rejected", p)
		}
	}
}

func TestValidatePublishPath_UnsetKeysDirMatchesNothing(t *testing.T) {
	cfg := &config.Config{RepoRoot: "/repo/root"} // KeysDir left empty (misconfigured repo)
	// Must not match everything just because dir=="" - that would deny all publishes outright.
	if err := validatePublishPath(cfg, "data/penguins.csv"); err != nil {
		t.Fatalf("expected an ordinary path to still be allowed when keys_dir is unset, got: %v", err)
	}
}
