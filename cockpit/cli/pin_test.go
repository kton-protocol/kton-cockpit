package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoOf(t *testing.T) {
	for in, want := range map[string]string{
		"python:3.12-slim":                   "python",
		"python@sha256:ab":                   "python",
		"ghcr.io/org/img:1.0":                "ghcr.io/org/img",
		"localhost:5000/img":                 "localhost:5000/img",
		"localhost:5000/img:tag@sha256:ab":   "localhost:5000/img",
		"docker.io/library/python:3.12-slim": "docker.io/library/python",
	} {
		if got := repoOf(in); got != want {
			t.Errorf("repoOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeEngine answers `image inspect` with the given RepoDigests JSON, as docker would.
func fakeEngine(t *testing.T, digests string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "engine")
	must(t, os.WriteFile(p, []byte("#!/bin/sh\necho '"+digests+"'\n"), 0o755))
	return p
}

// The digest is chosen by the image's name, not by position; several for the name, or none at all,
// is refused rather than guessed.
func TestResolveDigest_ByNameOnly(t *testing.T) {
	ctx := context.Background()
	two := `["mirror.example/python@sha256:11","python@sha256:22"]`
	if got, err := resolveDigest(ctx, fakeEngine(t, two), "python:3.12-slim"); err != nil || got != "python@sha256:22" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := resolveDigest(ctx, fakeEngine(t, `[]`), "built-here:latest"); err == nil || !strings.Contains(err.Error(), "never pushed") {
		t.Errorf("an image without a registry digest was pinned: %v", err)
	}
	if _, err := resolveDigest(ctx, fakeEngine(t, `["other@sha256:33"]`), "python:3.12-slim"); err == nil {
		t.Error("a digest for another name was taken")
	}
}
