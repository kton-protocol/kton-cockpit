package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRedacted_ScrubsSecretFromErrorOnFailure(t *testing.T) {
	dir := t.TempDir()
	const secret = "supersecrettoken12345"

	// A fake "git" that echoes its args (including the secret) to stderr, then fails — standing
	// in for a real git push failure that would otherwise echo the auth header verbatim.
	fakeGit := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho \"failed with arg: $2\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runRedacted(context.Background(), dir, fakeGit, []string{"push", secret}, secret)
	if err == nil {
		t.Fatal("expected an error from the failing fake git")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked into error message: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected a [REDACTED] placeholder in place of the secret, got: %v", err)
	}
}

func TestRunRedacted_NoSecretMeansNoRedaction(t *testing.T) {
	dir := t.TempDir()
	fakeGit := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho \"plain failure\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runRedacted(context.Background(), dir, fakeGit, []string{"push"}, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "plain failure") {
		t.Fatalf("expected the real output preserved when there's no secret to redact, got: %v", err)
	}
}

func TestRedactSlice(t *testing.T) {
	in := []string{"-c", "http.extraheader=AUTHORIZATION: basic abc123secret", "push"}
	out := redactSlice(in, "abc123secret")
	joined := strings.Join(out, " ")
	if strings.Contains(joined, "abc123secret") {
		t.Fatalf("secret survived redaction: %q", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder, got: %q", joined)
	}
}
