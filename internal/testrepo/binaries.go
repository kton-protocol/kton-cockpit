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
//  3. built on demand from a kton checkout (KTON_SRC, else a sibling ./kton directory) into
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
	if exists(plankton) && exists(nekton) && exists(kton) {
		return plankton, nekton, kton
	}

	src := ktonSrc(t)
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
	// kton too: `cockpit show` reads the records through `kton serve` rather than parsing the
	// registry, so the tests need it for the same reason the product does.
	build(t, src, kton, "./kton/reference/cmd/kton")
	return plankton, nekton, kton
}

// ktonSrc locates a kton checkout to build the kernel from, or returns "" if there is none.
func ktonSrc(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("KTON_SRC")}
	candidates = append(candidates, filepath.Join(filepath.Dir(cockpitRoot(t)), "kton"))
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
