package binaries

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// stubPlankton writes a fake plankton that answers `reproduces` with the given stdout and exit code.
func stubPlankton(t *testing.T, stdout string, exit int) *Runner {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'J'\n" + stdout + "\nJ\nexit " + itoa(exit) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "plankton"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return New(&config.Config{RepoRoot: dir, BinDir: dir})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

// A genuine non-match exits 1 — and so does a usage error. Before the verdict was JSON the two were
// indistinguishable, so a broken invocation was reported to the caller as "these outputs differ".
// The answer is what separates them now, not the exit code.
func TestReproduces_ANonMatchIsAnAnswerDespiteExitOne(t *testing.T) {
	r := stubPlankton(t, `{"level": null, "matched": false, "via": null}`, 1)
	res, err := r.Reproduces(context.Background(), "sha256:a", "sha256:b", "")
	if err != nil {
		t.Fatalf("a verdict of 'they do not match' is an answer, not a failure: %v", err)
	}
	if res.Matched || res.Level != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestReproduces_NoVerdictIsAFailureNotANonMatch(t *testing.T) {
	r := stubPlankton(t, "error: usage: plankton reproduces <ref> <cand>", 1)
	if _, err := r.Reproduces(context.Background(), "sha256:a", "", ""); err == nil {
		t.Fatal("a usage error must not be reported as 'the outputs do not match'")
	}
}

// The property the deleted regex tests existed to protect, kept: plankton compares ref == cand
// BEFORE consulting a normalizer, so identical bytes are L0 even when --via was passed. Inferring
// "L1 whenever via was given" mislabels a genuine L0 in any repo with a default normalizer, and an
// L0 policy then rejects a reproduction that was correct.
func TestReproduces_IdenticalBytesAreL0EvenWithViaPassed(t *testing.T) {
	r := stubPlankton(t, `{"level": "L0", "matched": true, "via": null}`, 0)
	res, err := r.Reproduces(context.Background(), "sha256:a", "sha256:a", "sha256:normalizer")
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != "L0" {
		t.Fatalf("level: %q — it is read from plankton's answer, never inferred from --via", res.Level)
	}
}

func TestReproduces_ANormalizerMatchIsL1(t *testing.T) {
	r := stubPlankton(t, `{"level": "L1", "matched": true, "via": "sha256:normalizer"}`, 0)
	res, err := r.Reproduces(context.Background(), "sha256:a", "sha256:b", "sha256:normalizer")
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != "L1" || res.Via == "" {
		t.Fatalf("got %+v", res)
	}
}
