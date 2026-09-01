package binaries

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// stubKernel writes fake plankton/nekton binaries that report the given version strings, so the
// check can be exercised against an old kernel without keeping a 0.1 binary around.
func stubKernel(t *testing.T, planktonSays, nektonSays string) *Runner {
	t.Helper()
	dir := t.TempDir()
	for name, says := range map[string]string{"plankton": planktonSays, "nekton": nektonSays} {
		script := "#!/bin/sh\necho '" + says + "'\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return New(&config.Config{RepoRoot: dir, BinDir: dir})
}

func TestCheckKernel_AcceptsTheRequiredVersion(t *testing.T) {
	r := stubKernel(t, "plankton 0.2 (reference)", "nekton 0.2 (reference)")
	if err := r.CheckKernel(context.Background()); err != nil {
		t.Fatalf("0.2 should be accepted: %v", err)
	}
}

func TestCheckKernel_AcceptsNewerThanRequired(t *testing.T) {
	r := stubKernel(t, "plankton 1.0 (reference)", "nekton 0.3 (reference)")
	if err := r.CheckKernel(context.Background()); err != nil {
		t.Fatalf("newer kernels should be accepted: %v", err)
	}
}

// The case this exists for: the participant template on GitHub vendors 0.1 binaries, which reject
// `about --json`, `by --json` and `reproductions --trust-keys` on their usage line. Discovered as
// a usage error mid-call that reads like a syntax mistake; it has to be named as a version
// problem instead.
func TestCheckKernel_RejectsAnOldKernelAndSaysWhy(t *testing.T) {
	for name, r := range map[string]*Runner{
		"plankton old": stubKernel(t, "plankton 0.1 (reference)", "nekton 0.2 (reference)"),
		"nekton old":   stubKernel(t, "plankton 0.2 (reference)", "nekton 0.1 (reference)"),
		"both old":     stubKernel(t, "plankton 0.1 (reference)", "nekton 0.1 (reference)"),
	} {
		t.Run(name, func(t *testing.T) {
			err := r.CheckKernel(context.Background())
			if err == nil {
				t.Fatal("expected a 0.1 kernel to be rejected")
			}
			// The message has to be actionable, not just negative.
			for _, want := range []string{"0.1", "--json", "--trust-keys", "kton checkout"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q: %v", want, err)
				}
			}
		})
	}
}

func TestCheckKernel_UnreadableVersionIsAnError(t *testing.T) {
	r := stubKernel(t, "plankton (reference)", "nekton 0.2 (reference)")
	if err := r.CheckKernel(context.Background()); err == nil {
		t.Fatal("a binary whose version cannot be read must not pass the check")
	}
}
