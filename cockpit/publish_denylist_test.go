package cockpit

import (
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func denylistTestConfig() *config.Config {
	return config.Layout("/repo/root", config.Raw{})
}

func TestValidatePublishPath_AllowsOrdinaryRepoRelativePaths(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{
		"data/runs.csv",
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
	for _, p := range []string{"/etc/passwd", "/repo/root/data/runs.csv"} {
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

// Everything kton is under .kton/ (ADR-016): the configuration, the registries, the public keys and
// this checkout's local state. Publish writes none of it, in any spelling of the folder's name.
func TestValidatePublishPath_RejectsTheKtonFolder(t *testing.T) {
	cfg := denylistTestConfig()
	for _, p := range []string{".kton", ".kton/cockpit.config.json", ".kton/keys/self/session-1.pub",
		".kton/local/identity.json", ".kton/plankton/objects/x.json", ".KTON/cockpit.config.json",
		":(top).kton/local", ":/.kton/local/identity.json"} { // pathspec magic: kton-review L1 on #143
		if err := validatePublishPath(cfg, p); err == nil {
			t.Errorf("expected %q under .kton/ to be rejected", p)
		}
	}
}

func TestValidatePublishPath_DoesNotFalsePositiveOnSimilarlyNamedDir(t *testing.T) {
	// ".kton2" and "keys" are ordinary folders now: only .kton/ itself is the cockpit's.
	cfg := denylistTestConfig()
	for _, p := range []string{".kton2/file.txt", "keys/notes.txt", "cockpit.config.json"} {
		if err := validatePublishPath(cfg, p); err != nil {
			t.Errorf("expected %s to be allowed, got: %v", p, err)
		}
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

func TestValidatePublishPath_RejectsDotKeyExtensionAnywhere(t *testing.T) {
	cfg := denylistTestConfig()
	// Not under .kton/ at all - the *.key check is a floor of protection wherever a key turns up.
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
