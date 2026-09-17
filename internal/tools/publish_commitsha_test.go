package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resultText(result *mcp.CallToolResult) string {
	if result == nil {
		return "<nil result>"
	}
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestPublish_ReturnsTheActualFinalCommitSHA is a regression test for a real bug found while
// independently validating Michael's review fixes against a fresh kton-protocol/kton build:
// Publish does two SEPARATE commits (inputs+outputs, then the signed registry entry), but
// discarded the second commit's sha entirely (`if _, err := gitops.CommitAndPush(...)`) and
// returned the FIRST commit's sha as CommitSHA — which is genuinely stale, not the repo's actual
// HEAD by the time Publish returns. Confirmed live: `git ls-remote origin HEAD` disagreed with
// the reported CommitSHA. This drives the real Publish() handler against a real local git repo
// (real add/commit/rev-parse; only `push` is faked as a no-op, since it targets a well-formed but
// nonexistent github.com URL) and confirms the returned CommitSHA matches the repo's actual final
// HEAD after both commits.
func TestPublish_ReturnsTheActualFinalCommitSHA(t *testing.T) {
	repoDir := t.TempDir()
	// bin_dir is documented as repo-relative (cockpit.config.schema.json: "Repo-relative paths"),
	// and config.Load's resolution (filepath.Join(repoRoot, raw.Paths.BinDir)) only actually
	// behaves sensibly for a relative value — joining an absolute path as the second argument
	// nests it under repoRoot instead of using it as-is. The one binary this test still stands in
	// for is `git`, and it lives there, matching how a real participant repo is laid out.
	binDir := filepath.Join(repoDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-q")
	runGit("remote", "add", "origin", "https://github.com/testowner/testrepo.git")

	// Fake `git` wrapper: pass every subcommand through to the REAL git except `push`, which
	// would otherwise try (and fail) to reach the nonexistent github.com URL above. Everything
	// else — add, commit, rev-parse, remote get-url, diff — runs for real, so this test exercises
	// genuine two-commit HEAD advancement, not a fabricated sha sequence.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no real git on PATH")
	}
	fakeGit := filepath.Join(binDir, "git")
	script := "#!/bin/sh\nif [ \"$1\" = \"push\" ]; then exit 0; fi\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The kernel is linked in, so there is no plankton to fake — `author` really signs and really
	// writes into `registry/plankton`, which is what makes the SECOND CommitAndPush call have
	// something to stage. That matters for this test specifically: if both calls took the "nothing
	// staged" branch it could not distinguish a stale first-commit sha from the correct final one
	// at all (caught live: an earlier version of this test passed against the pre-fix code for
	// exactly that reason). So a real signing key, in the shape `keys/` holds them — a hex seed.
	if err := os.MkdirAll(filepath.Join(repoDir, "keys"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"session-1.key", "session-1-claims.key"} {
		if err := os.WriteFile(filepath.Join(repoDir, "keys", name),
			[]byte(hex.EncodeToString(seed)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfgJSON := `{
  "repo": {"owner": "testowner", "name": "testrepo"},
  "paths": {"plankton_dir": "registry/plankton", "nekton_dir": "registry/nekton", "templates_dir": "templates", "keys_dir": "keys", "bin_dir": "bin"},
  "identity": {"session_id": "session-1", "plankton_key": "keys/session-1.key", "nekton_key": "keys/session-1-claims.key"},
  "verbs": {"publish": true, "say": true, "ask": true},
  "claims": {"allowedTemplates": ["reproduces", "working-on"]},
  "trust": {"tiers": {"self": []}},
  "reproduction": {"requiredLevel": "L0", "normalizer": ""}
}`
	if err := os.WriteFile(filepath.Join(repoDir, "cockpit.config.json"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "registry", "plankton"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "data.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "-A")
	runGit("-c", "user.name=t", "-c", "user.email=t@t.local", "commit", "-q", "-m", "scaffold")

	chdir(t, repoDir)

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data.csv"},
		Outputs: []string{"data.csv"},
		Cmd:     "noop",
	})
	if err != nil {
		t.Fatalf("Publish Go error: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("Publish refused: %s", resultText(result))
	}
	if out.CommitSHA == "" {
		t.Fatal("expected a non-empty CommitSHA")
	}

	actualHead := exec.Command("git", "rev-parse", "HEAD")
	actualHead.Dir = repoDir
	headOut, err := actualHead.Output()
	if err != nil {
		t.Fatal(err)
	}
	actualHeadSHA := strings.TrimSpace(string(headOut))

	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = repoDir
	logOut, _ := logCmd.Output()
	t.Logf("CommitSHA=%s actualHeadSHA=%s\ngit log:\n%s", out.CommitSHA, actualHeadSHA, logOut)

	if out.CommitSHA != actualHeadSHA {
		t.Fatalf("Publish returned a STALE CommitSHA: reported %s, but the repo's actual final HEAD is %s "+
			"(Publish makes two commits — inputs+outputs, then the registry entry — and must return the SECOND one)",
			out.CommitSHA, actualHeadSHA)
	}
}
