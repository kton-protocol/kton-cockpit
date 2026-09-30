package testrepo

import (
	"os"
	"path/filepath"
	"testing"
)

// go.mod names the kernel, and the cockpit links exactly that. The kernel CLIs the fixture and the
// examples drive are built from the same module, so the reference binaries and the linked library
// cannot be two different kernels. This asserts that rather than assuming it: a bin/ left behind by
// an earlier go.mod is the stale build that has broken this repository before, and it looks like a
// missing subcommand or an empty registry rather than like the kernel having moved.
func TestKernel_TheBinariesAreTheKernelGoModRequires(t *testing.T) {
	if os.Getenv("COCKPIT_TEST_PLANKTON") != "" {
		// An explicit override is the caller's choice and not second-guessed; there is no claim of
		// this repository's to check against it.
		return
	}
	plankton, nekton, kton := resolveBinaries(t)
	for mod, bin := range map[string]string{
		"kton.dev/plankton": plankton,
		"kton.dev/nekton":   nekton,
		"kton.dev/kton":     kton,
	} {
		want := RequiredKernel(t, mod)
		if got := KernelOf(bin, mod); got != want {
			t.Errorf("%s was built from %s %q, but go.mod requires %q", filepath.Base(bin), mod, got, want)
		}
	}
}
