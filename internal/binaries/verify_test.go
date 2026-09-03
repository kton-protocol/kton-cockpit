package binaries

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// These are regression tests for a real bug an independent review found: VerifyFoton/VerifyClaim
// treated ANY non-zero exit from `plankton|nekton verify` — including a real failure like a
// missing pubkey file (exit 1, "error: ...") — the same as plankton/nekton's own documented
// "UNVERIFIED - WRONG KEY" mismatch (exit 2, confirmed live against the reference binary). That
// silently turned a broken trust-tier config (e.g. a typo'd .pub path) into what looked exactly
// like a legitimate "this signer isn't trusted" verdict, with no error ever surfaced.

// writeFakeVerifyBinary writes an executable shell script named `name` under dir that prints
// stderr (if any) and exits with code — standing in for plankton/nekton's own verify subcommand
// without invoking the real binary.
func writeFakeVerifyBinary(t *testing.T, dir, name string, code int, stderr string) {
	t.Helper()
	script := "#!/bin/sh\n"
	if stderr != "" {
		script += "echo " + strconv.Quote(stderr) + " >&2\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runnerWithFakeBinary(t *testing.T, name string, code int, stderr string) *Runner {
	t.Helper()
	binDir := t.TempDir()
	writeFakeVerifyBinary(t, binDir, name, code, stderr)
	return New(&config.Config{RepoRoot: t.TempDir(), BinDir: binDir})
}

func TestVerifyFoton_ExitZeroIsValid(t *testing.T) {
	r := runnerWithFakeBinary(t, "plankton", 0, "")
	ok, _, err := r.VerifyFoton(context.Background(), "sha256:abc", "key.pub")
	if err != nil {
		t.Fatalf("unexpected error on exit 0: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on exit 0")
	}
}

func TestVerifyFoton_ExitTwoIsMismatchNotError(t *testing.T) {
	r := runnerWithFakeBinary(t, "plankton", 2, "UNVERIFIED - WRONG KEY: this key did not sign the record")
	ok, _, err := r.VerifyFoton(context.Background(), "sha256:abc", "key.pub")
	if err != nil {
		t.Fatalf("expected a genuine exit-2 mismatch to report no error, got: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a mismatch")
	}
}

func TestVerifyFoton_ExitOneIsARealErrorNotASilentMismatch(t *testing.T) {
	r := runnerWithFakeBinary(t, "plankton", 1, "error: encoding/hex: invalid byte: U+002F '/'")
	ok, _, err := r.VerifyFoton(context.Background(), "sha256:abc", "/does/not/exist.pub")
	if err == nil {
		t.Fatal("expected a real error to be surfaced for exit code 1, not silently swallowed as ok=false")
	}
	if ok {
		t.Fatal("expected ok=false alongside the surfaced error")
	}
}

func TestVerifyClaim_ExitZeroIsValid(t *testing.T) {
	r := runnerWithFakeBinary(t, "nekton", 0, "")
	ok, _, err := r.VerifyClaim(context.Background(), "sha256:abc", "key.pub")
	if err != nil {
		t.Fatalf("unexpected error on exit 0: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on exit 0")
	}
}

func TestVerifyClaim_ExitTwoIsMismatchNotError(t *testing.T) {
	r := runnerWithFakeBinary(t, "nekton", 2, "UNVERIFIED - WRONG KEY: this key did not sign the record")
	ok, _, err := r.VerifyClaim(context.Background(), "sha256:abc", "key.pub")
	if err != nil {
		t.Fatalf("expected a genuine exit-2 mismatch to report no error, got: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for a mismatch")
	}
}

func TestVerifyClaim_ExitOneIsARealErrorNotASilentMismatch(t *testing.T) {
	r := runnerWithFakeBinary(t, "nekton", 1, "error: encoding/hex: invalid byte: U+002F '/'")
	ok, _, err := r.VerifyClaim(context.Background(), "sha256:abc", "/does/not/exist.pub")
	if err == nil {
		t.Fatal("expected a real error to be surfaced for exit code 1, not silently swallowed as ok=false")
	}
	if ok {
		t.Fatal("expected ok=false alongside the surfaced error")
	}
}

func TestIsVerifyMismatch(t *testing.T) {
	exitCode := func(code int) error {
		cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
		return cmd.Run()
	}
	if !isVerifyMismatch(exitCode(2)) {
		t.Error("expected exit code 2 to be recognized as a mismatch")
	}
	if isVerifyMismatch(exitCode(1)) {
		t.Error("expected exit code 1 to NOT be recognized as a mismatch")
	}
	if isVerifyMismatch(errors.New("not an exec.ExitError at all")) {
		t.Error("expected a non-ExitError to NOT be recognized as a mismatch")
	}
	if isVerifyMismatch(nil) {
		t.Error("expected nil to NOT be recognized as a mismatch")
	}
}
