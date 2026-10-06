package source

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRepoWith(t *testing.T, dirs ...string) string {
	t.Helper()
	work := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", work, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(work, d, ".keep"), nil, 0o644)
	}
	run("add", "-A")
	run("commit", "-qm", "x")
	return work
}

func gitSpec(t *testing.T, url, path string) Spec {
	b, _ := json.Marshal(map[string]string{"name": "hub", "kind": "git", "url": url, "path": path})
	var s Spec
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// An aggregation repository's layout is found, whichever of the ones in use it has.
func TestGit_FindsTheLayout(t *testing.T) {
	for _, l := range [][]string{
		{"registry/plankton", "registry/nekton"},
		{"federation/plankton-data", "federation/nekton-data"},
		{"plankton-data"},
	} {
		repo := gitRepoWith(t, l...)
		regs, err := gitKind{}.Fetch(context.Background(), gitSpec(t, repo, ""), "", t.TempDir())
		if err != nil || len(regs) != 1 || filepath.Base(regs[0].PlanktonDir) != filepath.Base(l[0]) {
			t.Errorf("%v: got %v, %v", l, regs, err)
		}
	}
}

// The negative control: a repository holding no registry is said to, not read as empty.
func TestGit_ARepositoryWithoutRegistryIsSaid(t *testing.T) {
	repo := gitRepoWith(t, "docs")
	if _, err := (gitKind{}).Fetch(context.Background(), gitSpec(t, repo, ""), "", t.TempDir()); err == nil {
		t.Error("a repository without a registry was read")
	}
	if err := (gitKind{}).Validate(gitSpec(t, repo, "../x")); err == nil {
		t.Error("a path leaving the repository was accepted")
	}
}

// A cache holding a clone of another repository is not read as this source: the cache outlives
// configurations, and a source of the same name may have pointed elsewhere.
func TestGit_AClonOfAnotherURLIsReplaced(t *testing.T) {
	cache := t.TempDir()
	old := gitRepoWith(t, "registry/plankton")
	if _, err := (gitKind{}).Fetch(context.Background(), gitSpec(t, old, ""), "", cache); err != nil {
		t.Fatal(err)
	}
	now := gitRepoWith(t, "federation/plankton-data")
	regs, err := gitKind{}.Fetch(context.Background(), gitSpec(t, now, ""), "", cache)
	if err != nil || len(regs) != 1 || filepath.Base(regs[0].PlanktonDir) != "plankton-data" {
		t.Errorf("the old clone was read: %v, %v", regs, err)
	}
}
