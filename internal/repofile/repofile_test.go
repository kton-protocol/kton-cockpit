//go:build unix

package repofile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// kton-review S1/S2 on #148: a link out of the repository names the outside file and is refused when
// the caller signs; a FIFO, a device or a link to one is refused before it is read, so nothing hangs.
func TestHash_RefusesALinkOutAndAnythingButARegularFile(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write := func(p, s string) { os.MkdirAll(filepath.Dir(p), 0o755); os.WriteFile(p, []byte(s), 0o644) }
	write(filepath.Join(root, "data.txt"), "a\n")
	write(filepath.Join(outside, "secret.txt"), "pin 1234\n")
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link-out.txt"))
	os.Symlink("data.txt", filepath.Join(root, "link-in.txt"))
	os.Symlink("/dev/zero", filepath.Join(root, "zero"))
	os.Mkdir(filepath.Join(root, "dir"), 0o755)
	if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("a\n"))
	want := "sha256:" + hex.EncodeToString(sum[:])

	for _, p := range []string{"data.txt", "link-in.txt"} {
		if h, rep, err := Hash(root, p, true); err != nil || h != want || rep != p {
			t.Errorf("%s: got %s %s %v, want %s", p, h, rep, err, want)
		}
	}
	for p, why := range map[string]string{
		"link-out.txt": "outside this repository",
		"../x.txt":     "outside this repository",
		"fifo":         "not a regular file",
		"zero":         "outside this repository",
		"dir":          "is a directory",
	} {
		ch := make(chan error, 1)
		go func() { _, _, err := Hash(root, p, true); ch <- err }()
		select {
		case err := <-ch:
			if err == nil || !strings.Contains(err.Error(), why) {
				t.Errorf("%s: want refused with %q, got %v", p, why, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: Hash did not return within 2s (a read that never ends)", p)
		}
	}
	// Without containment (ask: a lookup, nothing signed) an outside file is read, a FIFO or a
	// device still is not.
	if _, _, err := Hash(root, "link-out.txt", false); err != nil {
		t.Errorf("link-out.txt without containment: %v", err)
	}
	for _, p := range []string{"fifo", "zero"} {
		if _, _, err := Hash(root, p, false); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("%s without containment: want not a regular file, got %v", p, err)
		}
	}
	if _, _, err := Hash(root, "missing.txt", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing.txt: want ErrNotFound, got %v", err)
	}
}
