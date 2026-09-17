package gitops

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
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

// TestCommitAndPush_StillPushesWhenNothingNewIsStaged is a regression test for a real bug an
// independent review found: the "nothing staged" branch returned the current HEAD sha without
// ever attempting a push. HEAD can already be ahead of origin from a PRIOR call whose commit
// succeeded but whose push then failed (network blip, auth hiccup) — every subsequent publish of
// the same already-committed bytes takes this exact branch, so without a push here that commit
// would be stranded on the local clone forever. Confirms `git push` is actually invoked even when
// `git diff --cached --quiet` reports nothing staged, in the (realistic) case this test models:
// HEAD and the upstream tracking ref (@{u}) disagree — a stranded local commit — via the fake
// git's rev-parse returning a different value for "HEAD" than for "@{u}".
func TestCommitAndPush_StillPushesWhenNothingNewIsStaged(t *testing.T) {
	repoDir := t.TempDir()
	binDir := t.TempDir()
	pushLog := filepath.Join(binDir, "push.log")

	script := `#!/bin/sh
if [ "$1" = "add" ]; then
  exit 0
fi
if [ "$1" = "diff" ]; then
  exit 0
fi
if [ "$1" = "push" ]; then
  echo "$@" >> "` + pushLog + `"
  exit 0
fi
if [ "$1" = "rev-parse" ]; then
  if [ "$2" = "HEAD" ]; then
    echo "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
  else
    echo "0000000000000000000000000000000000000000"
  fi
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

	if _, err := CommitAndPush(context.Background(), cfg, []string{"already-committed.csv"}, "test"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	logged, err := os.ReadFile(pushLog)
	if err != nil {
		t.Fatalf("git push was never invoked on the \"nothing new staged\" path: %v", err)
	}
	args := strings.Fields(string(logged))
	// args[0] is "push" itself; the rest must explicitly target origin/HEAD — see
	// TestPush_TargetsOriginHeadExplicitly for why a bare "push" isn't good enough.
	if len(args) < 3 || args[1] != "origin" || args[2] != "HEAD" {
		t.Fatalf(`expected "git push origin HEAD", got argv: %v`, args)
	}
}

// TestCommitAndPush_SkipsPushWhenAlreadyInSyncWithUpstream is the companion test to
// TestCommitAndPush_StillPushesWhenNothingNewIsStaged: an independent cold-session review pointed
// out that pushing unconditionally on every "nothing new staged" call turns every republish of
// already-in-sync content into a real network round-trip (previously a pure local no-op) — a real
// concern in a flaky/offline sandboxed connector runtime. Confirms `git push` is NOT invoked when
// HEAD already matches the upstream tracking ref.
func TestCommitAndPush_SkipsPushWhenAlreadyInSyncWithUpstream(t *testing.T) {
	repoDir := t.TempDir()
	binDir := t.TempDir()
	pushLog := filepath.Join(binDir, "push.log")

	script := `#!/bin/sh
if [ "$1" = "add" ]; then
  exit 0
fi
if [ "$1" = "diff" ]; then
  exit 0
fi
if [ "$1" = "push" ]; then
  echo "$@" >> "` + pushLog + `"
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

	if _, err := CommitAndPush(context.Background(), cfg, []string{"already-committed.csv"}, "test"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	if _, err := os.Stat(pushLog); err == nil {
		t.Fatal("expected git push to be skipped when HEAD already matches the upstream tracking ref")
	}
}

// TestPush_TargetsOriginHeadExplicitly is a regression test for a real bug an independent review
// found: a bare `git push` relies on ambient state the cockpit never controls or verifies — an
// already-configured upstream tracking branch, and whatever push.default happens to be in this
// environment. A fresh clone with no tracking branch set makes a bare push fail outright; a
// `matching` push.default could push OTHER local branches the cockpit never touched. "origin
// HEAD" pushes exactly the currently checked-out commit to the same-named branch on origin,
// unconditionally, regardless of local tracking/push.default state.
func TestPush_TargetsOriginHeadExplicitly(t *testing.T) {
	repoDir := t.TempDir()
	binDir := t.TempDir()
	pushLog := filepath.Join(binDir, "push.log")

	script := `#!/bin/sh
if [ "$1" = "push" ]; then
  echo "$@" >> "` + pushLog + `"
  exit 0
fi
exit 0
`
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := &config.Config{RepoRoot: repoDir}
	if err := push(context.Background(), cfg); err != nil {
		t.Fatalf("push: %v", err)
	}

	logged, err := os.ReadFile(pushLog)
	if err != nil {
		t.Fatalf("git push was never invoked: %v", err)
	}
	args := strings.Fields(string(logged))
	if len(args) < 3 || args[1] != "origin" || args[2] != "HEAD" {
		t.Fatalf(`expected "git push origin HEAD", got argv: %v`, args)
	}
}

// TestPush_RedactsBothRawTokenAndBase64EncodedHeaderOnFailure is a regression test for a critical
// bug an independent cold-session security review found: push()'s GITHUB_TOKEN/GH_TOKEN auth
// header is embedded in argv as "AUTHORIZATION: basic <base64(x-access-token:<token>)>" — base64
// encoding does not preserve substrings, so a redact pass matching only the RAW token string never
// matches anything in that argv element. Any push failure with token auth configured returned the
// fully intact, trivially-decodable token straight into the returned error (and from there,
// unredacted, into the MCP tool result shown to a session — precisely the sandboxed,
// less-trusted runtime this token mechanism exists for). This drives the real push() against a
// fake git that fails, and confirms the token is unrecoverable from the returned error by BOTH
// checks the original bug would have passed: the raw substring, AND the base64 blob actually
// embedded in argv.
func TestPush_RedactsBothRawTokenAndBase64EncodedHeaderOnFailure(t *testing.T) {
	repoDir := t.TempDir()
	binDir := t.TempDir()
	const token = "ghp_SUPER_SECRET_VALUE_1234567890"

	// "push" isn't $1 here: with token auth, the real argv is "-c http.extraheader=... push origin
	// HEAD" — "push" is the 3rd positional. Match it anywhere in argv instead of assuming a
	// position.
	script := `#!/bin/sh
case " $@ " in
  *" push "*)
    echo "push failed: simulated" >&2
    exit 1
    ;;
esac
exit 0
`
	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITHUB_TOKEN", token)

	cfg := &config.Config{RepoRoot: repoDir}
	err := push(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected push to fail (fake git exits 1 on push)")
	}

	errText := err.Error()
	if strings.Contains(errText, token) {
		t.Fatalf("raw token leaked into push error: %v", err)
	}
	// The actual leak vector the review found: the base64-encoded header, constructed the same
	// way push() builds it, must not survive into the error either.
	encodedHeader := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	if strings.Contains(errText, encodedHeader) {
		t.Fatalf("base64-encoded auth header leaked into push error (the actual production leak vector): %v", err)
	}
	if !strings.Contains(errText, "[REDACTED]") {
		t.Fatalf("expected a [REDACTED] placeholder in the error, got: %v", err)
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
