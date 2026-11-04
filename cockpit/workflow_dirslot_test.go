package cockpit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gitmick/ktonpkg"
)

func dirStep(t *testing.T) (string, *ktonpkg.Step, *ktonpkg.PlanStep) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"docs/images/a.png", "docs/images/sub/b.png", "docs/one.md"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &ktonpkg.Step{ID: "s", Inputs: []ktonpkg.Input{{Slot: "source.md", From: "hole:source"}, {Slot: "resources/", From: "hole:resources"}}}
	st := &ktonpkg.PlanStep{ID: "s",
		Stage:  []ktonpkg.Stage{{From: "docs/one.md", To: "w/s/source.md"}, {From: "docs/images", To: "w/s/resources"}},
		Inputs: []string{"w/s/resources", "w/s/source.md"}}
	return root, s, st
}

// Every file in a folder bound to a folder slot is an input under the slot, so a run records each
// one (SPEC §7.5).
func TestExpandDirSlots_EveryFileIsAnInput(t *testing.T) {
	root, s, st := dirStep(t)
	if err := expandDirSlots(root, s, st); err != nil {
		t.Fatal(err)
	}
	want := []string{"w/s/resources/a.png", "w/s/resources/sub/b.png", "w/s/source.md"}
	if !reflect.DeepEqual(st.Inputs, want) {
		t.Fatalf("inputs: want %v, got %v", want, st.Inputs)
	}
	if len(st.Stage) != 3 || st.Stage[2].From != "docs/images/sub/b.png" || st.Stage[2].To != "w/s/resources/sub/b.png" {
		t.Fatalf("stage: %+v", st.Stage)
	}
}

// A link in the folder could reach outside it, so it is refused rather than followed.
func TestExpandDirSlots_ALinkInTheFolderIsRefused(t *testing.T) {
	root, s, st := dirStep(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "docs/images/link.png")); err != nil {
		t.Skip(err)
	}
	if err := expandDirSlots(root, s, st); err == nil {
		t.Fatal("want refused")
	}
}

// An optional folder left open stages nothing; it must not read as the repository's root.
func TestExpandDirSlots_AFolderLeftOpenHasNoFiles(t *testing.T) {
	root, s, st := dirStep(t)
	st.Stage[1].From = ""
	if err := expandDirSlots(root, s, st); err != nil {
		t.Fatal(err)
	}
	if want := []string{"w/s/source.md"}; !reflect.DeepEqual(st.Inputs, want) {
		t.Fatalf("want %v, got %v", want, st.Inputs)
	}
}
