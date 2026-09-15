package testrepo

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// resolveBinaries finds the plankton and nekton binaries the fixture repo will use, in this
// order:
//
//  1. COCKPIT_TEST_PLANKTON / COCKPIT_TEST_NEKTON, if set — an explicit override for CI or for
//     testing against a specific kernel build.
//  2. bin/plankton and bin/nekton in this repo, if already built.
//  3. built on demand from a kton checkout (KTON_SRC, else a sibling ./kton-pinned, else ./kton) into
//     bin/, which is gitignored.
//
// It never falls back to a plankton/nekton found on PATH: which kernel build produced a store
// decides whether that store reads as populated or as empty-with-exit-0, so an ambient binary of
// unknown provenance is precisely the thing not to test against.
func resolveBinaries(t *testing.T) (plankton, nekton, kton string) {
	t.Helper()

	binDir := filepath.Join(cockpitRoot(t), "bin")
	plankton = filepath.Join(binDir, "plankton")
	nekton = filepath.Join(binDir, "nekton")
	kton = filepath.Join(binDir, "kton")
	if p, n := os.Getenv("COCKPIT_TEST_PLANKTON"), os.Getenv("COCKPIT_TEST_NEKTON"); p != "" && n != "" {
		plankton, nekton = p, n
	}
	src := ktonSrc(t)
	if exists(plankton) && exists(nekton) && exists(kton) && !stale(t, src, plankton, nekton, kton) {
		return plankton, nekton, kton
	}

	if src == "" {
		t.Fatalf("no plankton/nekton binaries and no kton source to build them from.\n"+
			"Either build them:  go build -o %s/plankton ./reference/cmd/plankton  (in a kton checkout)\n"+
			"or point KTON_SRC at a kton checkout, or set COCKPIT_TEST_PLANKTON/COCKPIT_TEST_NEKTON.", binDir)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("building plankton/nekton from %s into %s", src, binDir)
	build(t, src, plankton, "./reference/cmd/plankton")
	build(t, src, nekton, "./nekton/reference/cmd/nekton")
	// kton too: anchoring shells out to `kton anchor`, so the tests need it for the same reason the
	// product does. `cockpit show` no longer does — it reads `plankton records`/`nekton records`
	// since #85 — but the anchor path still holds this dependency.
	build(t, src, kton, "./kton/reference/cmd/kton")
	return plankton, nekton, kton
}

// stale reports whether the built binaries came from a different commit than the kton checkout
// currently holds.
//
// This exists because the alternative kept happening. bin/ is gitignored and reused across runs, so
// a checkout that moves on leaves binaries behind that still look fine — and the symptom is not a
// clear failure but a subcommand that does not exist yet, or worse, a store written by one kernel
// read by another. Go stamps vcs.revision into a binary built from a git tree, so the question
// "were these built from what is checked out now" has an actual answer rather than a habit of
// remembering to rebuild.
//
// With no checkout to compare against, nothing is stale: an explicitly provided binary
// (COCKPIT_TEST_PLANKTON) is the caller's choice and not second-guessed.
func stale(t *testing.T, src string, bins ...string) bool {
	t.Helper()
	if src == "" {
		return false
	}
	head, err := exec.Command("git", "-C", src, "rev-parse", "HEAD").Output()
	if err != nil {
		return false // not a git checkout, so there is nothing to be stale against
	}
	want := strings.TrimSpace(string(head))
	for _, bin := range bins {
		if buildRevision(bin) != want {
			t.Logf("rebuilding: %s was built from %.12s, %s is at %.12s",
				filepath.Base(bin), buildRevision(bin), src, want)
			return true
		}
	}
	return false
}

// buildRevision reads the commit Go stamped into a binary, or "" if it carries none.
func buildRevision(bin string) string {
	out, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "build" && f[1] == "vcs.revision" {
			return f[2]
		}
		if len(f) == 2 && f[0] == "build" && strings.HasPrefix(f[1], "vcs.revision=") {
			return strings.TrimPrefix(f[1], "vcs.revision=")
		}
	}
	return ""
}

// ktonSrc locates a kton checkout to build the kernel from, or returns "" if there is none.
func ktonSrc(t *testing.T) string {
	t.Helper()
	// A sibling `kton-pinned` is preferred over `kton`, and the difference is not cosmetic: the
	// second is somebody's WORKING TREE. Building from it makes every intermediate state of their
	// work a build input here, and a multi-step refactor upstream will at some point not compile —
	// which is not their mistake but this repository's, for reading a directory nobody promised
	// would be buildable. `kton-pinned` is a checkout this repo controls, parked on the commit
	// CLAUDE.md names, which is also what CI does. KTON_SRC still overrides both.
	candidates := []string{os.Getenv("KTON_SRC")}
	root := filepath.Dir(cockpitRoot(t))
	candidates = append(candidates,
		filepath.Join(root, "kton-pinned"),
		filepath.Join(root, "kton"))
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if exists(filepath.Join(c, "reference", "cmd", "plankton")) {
			return c
		}
	}
	return ""
}

func build(t *testing.T, srcDir, out, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = srcDir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s in %s: %v\n%s", pkg, srcDir, err, b)
	}
}

// cockpitRoot is this repository's root, derived from this source file's own location so it holds
// regardless of the process's working directory.
func cockpitRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine this source file's path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))) // internal/testrepo/binaries.go -> root
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// --git-dir for a bare repo, -C for a working tree: OriginSHA asks the bare repo directly.
	if strings.HasSuffix(dir, ".git") {
		args = append([]string{"--git-dir", dir}, args...)
		dir = filepath.Dir(dir)
	}
	return run(t, dir, "git", args...)
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s (in %s): %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}
