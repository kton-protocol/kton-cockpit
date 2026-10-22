package index_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// The stamp moves with every file a store reads, whatever its name and wherever a link points, and
// stands still for the bookkeeping that opening a registry rewrites.
func TestStamp_MovesWithWhatTheStoreReadsNotWithItsBookkeeping(t *testing.T) {
	root := t.TempDir()
	nk := filepath.Join(root, "nekton")
	linked := filepath.Join(root, "elsewhere")
	for _, d := range []string{filepath.Join(nk, "objects"), filepath.Join(root, "plankton", "objects"), linked} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(nk, "objects", "unscoped.nekton.jsonl"), "{}\n")
	write(filepath.Join(linked, "a.nekton.jsonl"), "{}\n")
	if err := os.Symlink(linked, filepath.Join(nk, "objects", "sub")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	in := []binaries.Input{{Name: "(own)", Registry: source.Registry{PlanktonDir: filepath.Join(root, "plankton"), NektonDir: nk}}}
	stamp := func() string {
		t.Helper()
		s, err := index.Stamp(in)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	later := func(p string) { // a write the clock can tell from the last one
		t.Helper()
		ts := time.Now().Add(time.Minute)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	was := stamp()
	for _, f := range []string{".seq", "objects/.format", "objects/.objects.lock", "objects/.tmp-x-1"} {
		write(filepath.Join(nk, f), "1\n")
	}
	if now := stamp(); now != was {
		t.Fatal("the store's bookkeeping moved the stamp")
	}

	for name, change := range map[string]func(){
		"a dot-named claim file": func() { write(filepath.Join(nk, "objects", ".x.nekton.jsonl"), "{}\n") },
		"a change behind a link": func() {
			write(filepath.Join(linked, "a.nekton.jsonl"), "{}\n{}\n")
			later(filepath.Join(linked, "a.nekton.jsonl"))
		},
		"a record added": func() { write(filepath.Join(root, "plankton", "objects", "r.json"), "{}") },
	} {
		before := stamp()
		change()
		if stamp() == before {
			t.Errorf("%s left the stamp as it was", name)
		}
	}
}

// A link back up the tree ends the walk instead of looping.
func TestStamp_ALinkLoopEnds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nekton", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nekton"), filepath.Join(root, "nekton", "objects", "up")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := index.Stamp([]binaries.Input{{Registry: source.Registry{NektonDir: filepath.Join(root, "nekton")}}}); err != nil {
		t.Fatal(err)
	}
}
