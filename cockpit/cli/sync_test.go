package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// cockpit sync brings in what another clone shared, through the backend — the command a participant
// runs instead of git pull, in whatever mode.
func TestSync_TakesInWhatAnotherCloneShared(t *testing.T) {
	r := testrepo.New(t)
	gitIn := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// An origin the guard reads as github.com/<owner>/<name> and git can still reach: a local bare
	// repository at a path of that shape. (Rewriting the URL instead would change what the guard
	// reads too, and it would rightly refuse.)
	origin := filepath.Join(t.TempDir(), "github.com", testrepo.Owner, testrepo.Name+".git")
	gitIn("", "clone", "--quiet", "--bare", r.Origin, origin)
	gitIn(r.Root, "remote", "set-url", "origin", origin)
	gitIn(r.Root, "remote", "set-url", "--push", "origin", origin)
	gitIn(r.Root, "push", "--quiet", "-u", "origin", "HEAD:main")
	other := filepath.Join(t.TempDir(), "other")
	gitIn("", "clone", "--quiet", origin, other)
	gitIn(other, "-c", "user.name=o", "-c", "user.email=o@x", "commit", "--quiet", "--allow-empty", "-m", "theirs")
	gitIn(other, "push", "--quiet", "origin", "HEAD")
	theirs := gitIn(other, "rev-parse", "HEAD")

	out, code := r.Cockpit(t, "sync")
	if code != 0 || !strings.Contains(out, "now at") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if head := gitIn(r.Root, "rev-parse", "HEAD"); head != theirs {
		t.Fatalf("after sync HEAD is %s, want %s", head, theirs)
	}
	// The negative control: nothing new, nothing moves.
	if out, code := r.Cockpit(t, "sync"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("a second sync (exit %d):\n%s", code, out)
	}
}
