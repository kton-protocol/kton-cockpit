package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A private signing key whose mode lets other users read it is the one thing about a key path worth
// more than "it exists". Not hypothetical here: on a Windows drive mounted into WSL — where this
// repository itself lives — a 0600 request lands as 0777, and kton's keygen warns only at creation,
// on the stderr of a command the cockpit never runs.
//
// The decision is a pure function of the mode so it can be checked exhaustively and, more to the
// point, WITHOUT a filesystem. A table test that wrote real files would have to skip wherever modes
// are not held — which is precisely the environment this check exists for, so the one filesystem
// that most needs the test is the one that would silently not run it.
func TestKeyModeExposed(t *testing.T) {
	cases := map[os.FileMode]bool{
		0o600: false, // what a private key should be
		0o400: false, // read-only for the owner
		0o000: false, // unreadable by anyone, including the owner: not an exposure
		0o640: true,  // the group can read it
		0o604: true,  // everyone can read it
		0o620: true,  // the group can WRITE it — no better than reading
		0o601: true,  // other-execute is still a bit granted to someone else
		0o777: true,  // what a DrvFs mount produces
		0o666: true,
	}
	for perm, want := range cases {
		if got := keyModeExposed(perm); got != want {
			t.Errorf("mode %04o: exposed=%v want %v", perm, got, want)
		}
	}
}

// And the reporting around it, on a real file, so the wiring is covered too.
func TestCheckKeyPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session-1.key")
	if err := os.WriteFile(p, []byte("deadbeef"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Whether this filesystem HOLDS 0600 is not this test's business — it asserts that what
	// checkKeyPath reports agrees with the mode actually on disk, which is true either way and is
	// the real contract.
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	got := checkKeyPath(p)
	alarmed := strings.Contains(got, "READABLE BY OTHERS")
	if alarmed != keyModeExposed(fi.Mode().Perm()) {
		t.Fatalf("mode on disk is %v but checkKeyPath reported:\n%s", fi.Mode().Perm(), got)
	}
	if alarmed && !strings.Contains(got, "sign as this repo") {
		t.Errorf("the warning does not say what the exposure costs:\n%s", got)
	}
	if !alarmed && !strings.Contains(got, "[ok]") {
		t.Errorf("an unexposed key should read as ok:\n%s", got)
	}
}

// A key that is not there at all is reported as missing rather than as a mode problem — otherwise
// the most common setup mistake would be described as the rarer one.
func TestCheckKeyPath_MissingIsMissing(t *testing.T) {
	got := checkKeyPath(filepath.Join(t.TempDir(), "absent.key"))
	if !strings.Contains(got, "[MISSING]") {
		t.Fatalf("expected a missing key to be reported as missing, got: %s", got)
	}
}
