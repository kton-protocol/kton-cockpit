package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests drive the real tool handlers against a real, fully-configured participant repo
// (not a fabricated fixture) — the anti-wrong-folder guard genuinely checks its actual git
// remote. It has no published fotons yet (that happens interactively, later in the tutorial), so
// these exercise the "no data yet" and guard paths honestly rather than assuming specific hashes
// exist. If this path goes stale again (local demo repos get reorganized often), point it at
// whichever participant repo currently has a valid cockpit.config.json + keys.
const realParticipantRepo = "/mnt/c/dev/planktonReproduce/participant-christian"

// chdir changes to dir for the duration of the test. On this WSL/NTFS setup, os.Chdir into a
// since-deleted directory can return no error while leaving the process's cwd broken (os.Getwd
// then returns ""), instead of cleanly failing — so this checks Getwd too, not just Chdir's own
// error, before deciding the target is actually usable.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Skipf("repo not available in this environment: %v", err)
	}
	if wd, err := os.Getwd(); err != nil || wd == "" {
		t.Skipf("chdir to %q left the process cwd broken (wd=%q err=%v) — the directory likely no longer exists", dir, wd, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestAsk_ProducerOnUnknownHashAgainstRealRegistry(t *testing.T) {
	chdir(t, realParticipantRepo)

	// No foton has ever produced this hash — a made-up value, not a real digest.
	const unknownHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Ask reported a tool error for a plain not-found query: %+v", result.Content)
	}
	// plankton's own "(none) - <hash> is a lineage root or unknown" message echoes the queried
	// hash back into the raw text, so it legitimately shows up in Records — the guarantee we
	// actually care about is that it's correctly unverified and therefore excluded, not that it
	// never appears in the raw-text scrape at all.
	if len(out.Included) != 0 {
		t.Fatalf("expected nothing verified+included for an unknown hash, got %+v", out.Included)
	}
	t.Logf("producer query on unknown hash: records=%+v, filter=%q", out.Records, out.FilterApplied)
}

func TestAsk_ReproductionsOnUnknownHashAgainstRealRegistry(t *testing.T) {
	chdir(t, realParticipantRepo)

	const unknownHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "reproductions", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned Go error: %v", err)
	}

	// Reproductions() now always requires --trust-keys (Michael's review, blocking item 2): a
	// self-declared, forgeable signer count is worse than no count at all. The vendored plankton
	// binary in this real participant repo predates --trust-keys support on `reproductions`
	// (review item 9: no version/capability pinning on the vendored binaries), so against it the
	// query must fail loudly rather than silently hand back an unverified answer. Once the
	// vendored binary is upgraded to one that supports the flag, replace this with a success-path
	// assertion again (see git history for the pre-fix version of this test).
	if !result.IsError {
		t.Fatalf("expected a tool error: this repo's vendored plankton binary does not support --trust-keys on reproductions, so the query must fail rather than return a forgeable answer; got out=%+v", out)
	}
	t.Logf("reproductions on a --trust-keys-incapable binary correctly surfaced as an error: %+v", result.Content)
}

func TestAsk_UnknownQueryIsRejected(t *testing.T) {
	chdir(t, realParticipantRepo)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "delete-everything", Ref: "sha256:whatever"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for an unknown query verb")
	}
}

func TestConfigGuard_RefusesOutsideAnyGitRepo(t *testing.T) {
	chdir(t, os.TempDir())

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: "sha256:deadbeef"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected the anti-wrong-folder guard to refuse outside any git repository")
	}
}

// TestPublish_RefusesToCommitSigningKey is the integration-level companion to
// publish_denylist_test.go's unit tests: it drives the real Publish() handler (not just
// validatePublishPath in isolation) against a real repo that has a real signing key on disk
// (keys/session-1.key, referenced by this repo's own cockpit.config.json identity.plankton_key),
// and confirms two things a unit test on the pure function can't: (1) the denial actually happens
// inside Publish() before anything else runs, and (2) — most importantly — no git state changes
// at all: HEAD stays exactly where it was, proving the key was never staged, committed, or
// pushed, not just that an error was returned alongside a mutation that happened anyway.
func TestPublish_RefusesToCommitSigningKey(t *testing.T) {
	chdir(t, realParticipantRepo)

	beforeSHA := headSHA(t, realParticipantRepo)

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"keys/session-1.key"},
		Cmd:     "cat keys/session-1.key",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected publish to refuse committing a signing key, got a successful result: %+v", out)
	}

	afterSHA := headSHA(t, realParticipantRepo)
	if afterSHA != beforeSHA {
		t.Fatalf("HEAD changed (%s -> %s) even though publish should have refused before touching git at all",
			beforeSHA, afterSHA)
	}
}

// TestPublish_RefusesLeadingDashOutput is the integration-level regression test for the critical
// bypass an independent cold-session review found: gitops.CommitAndPush built `git add` with no
// "--" separator, so outputs: ["-f", "."] was parsed by git as `git add -f .` — a FLAG, not a
// literal path — force-adding every gitignored file in the repo (keys included) without the
// string "keys/..." ever appearing in the request. This is strictly worse than the hole Michael
// originally described (it doesn't even require naming a key) and would have sailed past every
// check in the original fix. Confirms both that the call is refused AND — the assertion that
// actually matters here — that nothing was force-staged into the index at all, not just that HEAD
// didn't move (a partial "staged but not committed" failure would pass a HEAD-only check).
func TestPublish_RefusesLeadingDashOutput(t *testing.T) {
	chdir(t, realParticipantRepo)

	beforeSHA := headSHA(t, realParticipantRepo)

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"-f", "."},
		Cmd:     "echo pwned",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected publish to refuse a leading-dash output path, got a successful result: %+v", out)
	}

	if afterSHA := headSHA(t, realParticipantRepo); afterSHA != beforeSHA {
		t.Fatalf("HEAD changed (%s -> %s) even though publish should have refused before touching git at all",
			beforeSHA, afterSHA)
	}
	if staged := stagedFiles(t, realParticipantRepo); staged != "" {
		t.Fatalf("expected nothing staged in the index, but found:\n%s", staged)
	}
}

func stagedFiles(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff --cached --name-only in %s: %v", repoDir, err)
	}
	return strings.TrimSpace(string(out))
}

func headSHA(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in %s: %v", repoDir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestConfigGuard_CockpitRepoDirEnvOverridesCwd(t *testing.T) {
	chdir(t, os.TempDir()) // cwd is deliberately NOT the participant repo

	t.Setenv("COCKPIT_REPO_DIR", realParticipantRepo)

	const unknownHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected COCKPIT_REPO_DIR to make this resolve against the real repo, got: %+v", result.Content)
	}
}
