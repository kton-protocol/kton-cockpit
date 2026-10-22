package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A repository before its first commit still answers: what is staged is tracked, nothing differs.
// doctor asks this right after `cockpit init`, which is exactly when there is no commit yet.
func TestTracked_BeforeTheFirstCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a")
	tracked, changed, err := Tracked(ctx, dir, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !tracked["a"] || tracked["b"] || len(changed) != 0 {
		t.Fatalf("tracked=%v changed=%v", tracked, changed)
	}
}
