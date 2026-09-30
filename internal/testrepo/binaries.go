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
//  1. COCKPIT_TEST_PLANKTON / COCKPIT_TEST_NEKTON, if set — an explicit override for testing
//     against a specific kernel build.
//  2. bin/plankton and bin/nekton in this repo, if already built from the kernel go.mod requires.
//  3. built on demand from exactly that module into bin/, which is gitignored.
//
// go.mod is the one place that names the kernel. The binaries are built from the same module
// version the cockpit links, so "the CLI and the library are the same kernel" holds by
// construction rather than by a second line someone keeps in step.
//
// It never falls back to a plankton/nekton found on PATH: which kernel build produced a store
// decides whether that store reads as populated or as empty-with-exit-0, so an ambient binary of
// unknown provenance is precisely the thing not to test against.
func resolveBinaries(t *testing.T) (plankton, nekton, kton string) {
	t.Helper()

	root := cockpitRoot(t)
	binDir := filepath.Join(root, "bin")
	plankton = filepath.Join(binDir, "plankton")
	nekton = filepath.Join(binDir, "nekton")
	kton = filepath.Join(binDir, "kton")
	if p, n := os.Getenv("COCKPIT_TEST_PLANKTON"), os.Getenv("COCKPIT_TEST_NEKTON"); p != "" && n != "" {
		return p, n, kton
	}

	bins := []struct{ out, mod, pkg string }{
		{plankton, "kton.dev/plankton", "kton.dev/plankton/cmd/plankton"},
		{nekton, "kton.dev/nekton", "kton.dev/nekton/cmd/nekton"},
		// kton too: anchoring drives `kton anchor`'s sigstore package, and the tests need the CLI
		// for the same reason the examples do.
		{kton, "kton.dev/kton", "kton.dev/kton/cmd/kton"},
	}
	for _, b := range bins {
		want := RequiredKernel(t, b.mod)
		if exists(b.out) && KernelOf(b.out, b.mod) == want {
			continue
		}
		if exists(b.out) {
			t.Logf("rebuilding: %s was built from %s %s, go.mod requires %s",
				filepath.Base(b.out), b.mod, KernelOf(b.out, b.mod), want)
		}
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		build(t, root, b.out, b.pkg)
	}
	return plankton, nekton, kton
}

// RequiredKernel is the version of a kton module this repository's go.mod requires, as Go resolves
// it — a replace directive included, so a local override is reported as what it is.
func RequiredKernel(t *testing.T, mod string) string {
	t.Helper()
	return run(t, cockpitRoot(t), "go", "list", "-m", "-f",
		"{{if .Replace}}{{.Replace.Path}}{{with .Replace.Version}} {{.}}{{end}}{{else}}{{.Version}}{{end}}", mod)
}

// KernelOf reads which version of a kton module a binary was built from, or "" if it carries no
// build information. A binary built as a dependency package records the module on a `dep` line,
// and on a `=>` line after it when a replace directive was in force.
func KernelOf(bin, mod string) string {
	out, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) < 3 || (f[0] != "dep" && f[0] != "mod") || f[1] != mod {
			continue
		}
		if i+1 < len(lines) {
			if r := strings.Fields(lines[i+1]); len(r) >= 2 && r[0] == "=>" {
				if len(r) >= 3 && !strings.HasPrefix(r[2], "h1:") {
					return r[1] + " " + r[2]
				}
				return r[1]
			}
		}
		return f[2]
	}
	return ""
}

func build(t *testing.T, dir, out, pkg string) {
	t.Helper()
	t.Logf("building %s into %s", pkg, out)
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s in %s: %v\n%s", pkg, dir, err, b)
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
