package testrepo

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// files lists every path under dir with its size, so a write of any kind shows.
func files(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if fi, err := d.Info(); err == nil && !d.IsDir() {
			rel += " " + strconv.FormatInt(fi.Size(), 10)
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func withSource(t *testing.T, cfg *config.Config, name, path string) {
	t.Helper()
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": name, "kind": "dir", "path": path})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	cfg.Raw.Federation.Sources = append(cfg.Raw.Federation.Sources, spec)
}

// Reading a federation source does not write into it: no numbering is persisted, no registry is
// created where there was none (kton-review W1 on PR #10). A source with no registry at all is
// refused rather than read as an empty one.
func TestFederation_ReadingASourceLeavesItAsItWas(t *testing.T) {
	ctx := context.Background()
	alice := New(t)
	ac := alice.Config(t)
	in := work(t, alice)
	a, err := binaries.New(ac).Author(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh clone of her registry: the records, as git brings them, and nothing else.
	clone := filepath.Join(t.TempDir(), "clone")
	if err := os.MkdirAll(filepath.Join(clone, "registry", "plankton"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-r", filepath.Join(alice.Root, "registry", "plankton", "objects"),
		filepath.Join(clone, "registry", "plankton")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	before := files(t, clone)

	bob := New(t)
	bc := bob.Config(t)
	withSource(t, bc, "alice", clone)
	d, err := binaries.New(bc).FotonByID(ctx, a.ID)
	if err != nil || d.ID != a.ID {
		t.Fatalf("bob does not read alice's record through the source: %v", err)
	}
	if _, _, err := binaries.ReadDirs(ctx, bc); err != nil {
		t.Fatal(err)
	}
	if after := files(t, clone); after != before {
		t.Fatalf("reading the source wrote into it:\nbefore\n%s\nafter\n%s", before, after)
	}

	withSource(t, bc, "typo", filepath.Join(t.TempDir(), "no-such-repo"))
	if _, _, err := binaries.ReadDirs(ctx, bc); err == nil || !strings.Contains(err.Error(), "no registry") {
		t.Fatalf("a source with no registry was read as an empty one: %v", err)
	}
	// The index reads the same inputs, so it refuses the same source (kton-review V2 on PR #11).
	if _, err := binaries.Inputs(ctx, bc); err == nil || !strings.Contains(err.Error(), "no registry") {
		t.Fatalf("the union's inputs include a source with no registry: %v", err)
	}
}

// The own registry before the first publish is not there yet, and that is no error: a union with
// sources is read all the same.
func TestFederation_AnOwnRegistryNotYetWrittenIsEmptyNotRefused(t *testing.T) {
	ctx := context.Background()
	alice, bob := New(t), New(t)
	bc := bob.Config(t)
	for _, d := range []string{bc.PlanktonDir, bc.NektonDir} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
	}
	withSource(t, bc, "alice", alice.Root)
	if _, _, err := binaries.ReadDirs(ctx, bc); err != nil {
		t.Fatalf("a union over an own registry not yet written was refused: %v", err)
	}
}
