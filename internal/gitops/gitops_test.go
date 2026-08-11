package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

func TestRunRedacted_ScrubsSecretFromErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	const secret = "supersecrettoken12345"

	// A fake "git" that echoes its args (including the secret) to stderr, then fails — standing
	// in for a real git push failure that would otherwise echo the auth header verbatim.
	fakeGit := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho \"failed with arg: $2\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runRedacted(context.Background(), dir, fakeGit, []string{"push", secret}, secret)
	if err == nil {
		t.Fatal("expected an error from the failing fake git")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked into error message: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected a [REDACTED] placeholder in place of the secret, got: %v", err)
	}
}

func TestRunRedacted_NoSecretMeansNoRedaction(t *testing.T) {
	dir := t.TempDir()
	fakeGit := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho \"plain failure\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runRedacted(context.Background(), dir, fakeGit, []string{"push"}, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "plain failure") {
		t.Fatalf("expected the real output preserved when there's no secret to redact, got: %v", err)
	}
}

// TestCommitAndPush_AddInvocationUsesDashDashSeparator is a regression test for the critical
// bypass an independent cold-session review found: without a "--" separator, a path starting with
// "-" (e.g. "-f") is parsed by git as a FLAG rather than a literal path — outputs: ["-f", "."]
// became `git add -f .`, force-adding every gitignored file in the repo (real signing keys
// included) without the string "keys/..." ever appearing in the published paths. This drives the
// real CommitAndPush against a fake `git` on PATH that just logs its "add" invocation's exact
// argv, so the assertion is against what git actually receives, not a re-implementation of the
// argument-building logic.
func TestCommitAndPush_AddInvocationUsesDashDashSeparator(t *testing.T) {
	repoDir := t.TempDir()
	binDir := t.TempDir()
	logFile := filepath.Join(binDir, "add-args.log")

	// Handles exactly the subcommands CommitAndPush issues on the "nothing staged" path (the
	// simplest path through the function, and the only one this test needs): add, diff
	// --cached --quiet (exit 0 = nothing staged, matching this test's empty/no-op paths list),
	// and rev-parse HEAD (CurrentSHA). Logs only the "add" invocation's argv.
	script := `#!/bin/sh
if [ "$1" = "add" ]; then
  echo "$@" >> "` + logFile + `"
  exit 0
fi
if [ "$1" = "diff" ]; then
  exit 0
fi
if [ "$1" = "rev-parse" ]; then
  echo "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
  exit 0
fi
exit 0
`
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{
		RepoRoot: repoDir,
		Raw:      config.Raw{Identity: config.Identity{SessionID: "session-1"}},
	}

	if _, err := CommitAndPush(context.Background(), cfg, []string{"-f", "."}, "test"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("fake git's add invocation was never logged: %v", err)
	}
	args := strings.Fields(string(logged))
	// args[0] is "add" itself (from `echo "$@"`); args[1] must be the "--" separator, BEFORE any
	// of the caller-supplied paths — that's what makes "-f" a literal path, not a flag.
	if len(args) < 2 || args[1] != "--" {
		t.Fatalf(`expected "git add -- ..." (separator immediately after add), got argv: %v`, args)
	}
}

func TestRedactSlice(t *testing.T) {
	in := []string{"-c", "http.extraheader=AUTHORIZATION: basic abc123secret", "push"}
	out := redactSlice(in, "abc123secret")
	joined := strings.Join(out, " ")
	if strings.Contains(joined, "abc123secret") {
		t.Fatalf("secret survived redaction: %q", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder, got: %q", joined)
	}
}
