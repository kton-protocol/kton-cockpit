package toolcall

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The CAS keeps a file only with the bytes it was observed with.
func TestCAS_KeepsOnlyTheObservedBytes(t *testing.T) {
	dir := t.TempDir()
	c := &CAS{Dir: filepath.Join(dir, "cas")}
	f := filepath.Join(dir, "run1.ext")
	os.WriteFile(f, []byte("OFV 123"), 0o644)
	sum := "ab2f7ee1e7c4f9f3a1d6ea1a8b5c1f0f5e0b8f4e3b2f8b4f4e1b1e0f7c9a6d21" // not the hash of these bytes
	if ok, err := c.Put(f, sum); ok || err != nil || c.Has(sum) {
		t.Fatalf("kept bytes under a hash they do not have: %v %v", ok, err)
	}
	s := sha256.Sum256([]byte("OFV 123"))
	h := hex.EncodeToString(s[:])
	if ok, err := c.Put(f, h); !ok || err != nil || !c.Has(h) {
		t.Fatalf("not kept: %v %v", ok, err)
	}
	if b, _ := os.ReadFile(c.Path(h)); string(b) != "OFV 123" {
		t.Fatalf("content %q", b)
	}
}

// Review R2 of #12: the command line splits as the shim quotes it, the inverse of shellJoin.
func TestSplitCmdline_UndoesTheShimsQuoting(t *testing.T) {
	for _, args := range [][]string{{"nmfe75", "run 1.mod", "run 1.lst"}, {"R", "-e", "it's"}, {"x", "", "a'b'c"}, {"plain", "args"}} {
		if got := SplitCmdline(ShellJoin(args)); strings.Join(got, "\x00") != strings.Join(args, "\x00") {
			t.Errorf("%q → %q → %q", args, ShellJoin(args), got)
		}
	}
	if got := NormCmdline("/opt/nm/run/nmfe75 'run 1.mod' 'run 1.lst'", "/w"); len(got) != 3 || got[1] != "run 1.mod" {
		t.Fatalf("argv %q", got)
	}
}

func TestShellJoin_QuotesWhatAShellWouldSplit(t *testing.T) {
	if got := ShellJoin([]string{"nmfe75", "run 1.mod", "it's", "-x=a/b"}); got != `nmfe75 'run 1.mod' 'it'\''s' -x=a/b` {
		t.Fatalf("%s", got)
	}
}

// Review R1 of #12: a path inside the repository stays that path, relative to the call's
// directory even above it; only a path outside the repository becomes its file name. Before,
// all three rows lost their directory and the record named files that are not there.
func TestNormArgv_RepoPathsKeepTheirPlaceOnlyOutsidePathsLoseIt(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "models/run1/.keep", "")
	r.Write(t, "analysis/.keep", "")
	root, _ := filepath.EvalSymlinks(r.Root)
	for _, row := range []struct{ cmdline, cwd, want string }{
		{"/opt/nm/run/nmfe75 " + root + "/data/pk.mod run1.lst", root + "/models/run1", "nmfe75 ../../data/pk.mod run1.lst"},
		{"Rscript " + root + "/R/fit.R", root + "/analysis", "Rscript ../R/fit.R"},
		{"/usr/bin/execute run1.mod --data=" + root + "/data/pk.csv", root + "/models", "execute run1.mod --data=../data/pk.csv"},
		{"/opt/nm/run/nmfe75 run1.mod -licfile=/opt/nm/license/nonmem.lic", root + "/models", "nmfe75 run1.mod -licfile=nonmem.lic"},
	} {
		if got := strings.Join(NormCmdline(row.cmdline, row.cwd), " "); got != row.want {
			t.Errorf("%s (in %s):\n got  %s\n want %s", row.cmdline, row.cwd, got, row.want)
		}
	}
}

// Re-check of #12: a submodule's .git file does not end the repository; the parent's root counts.
func TestRepoRoot_ASubmoduleIsPartOfItsParent(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{".git", "lib/sub/models"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "lib/sub/.git"), []byte("gitdir: ../../.git/modules/sub\n"), 0o644)
	if got := RepoRoot(filepath.Join(root, "lib/sub/models")); got != root {
		t.Fatalf("root %q, want the parent %q", got, root)
	}
	lone := t.TempDir() // a worktree: a .git file and nothing above
	os.MkdirAll(filepath.Join(lone, "a"), 0o755)
	os.WriteFile(filepath.Join(lone, ".git"), []byte("gitdir: /elsewhere\n"), 0o644)
	if got := RepoRoot(filepath.Join(lone, "a")); got != lone {
		t.Fatalf("worktree root %q, want %q", got, lone)
	}
}
