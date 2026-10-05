package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A starting point outside the repository is named by its absolute path. It used to be joined onto
// the repo root and reported as "not a directory".
func TestRunNew_FromOutsideTheRepo(t *testing.T) {
	r := testrepo.New(t)
	src := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(src, "inputs"), 0o755))
	must(t, os.WriteFile(filepath.Join(src, "inputs", "in.csv"), []byte("a\n1\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(src, "run.py"), []byte("print(1)\n"), 0o644))
	t.Chdir(r.Root)
	if err := runNew(context.Background(), []string{"first", "--from", src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "runs", "first", "inputs", "in.csv")); err != nil {
		t.Errorf("input not cloned: %v", err)
	}
}

// A refused run new leaves nothing: a half-made folder was taken for a run by `run <slug>`.
func TestRunNew_RefusedLeavesNoFolder(t *testing.T) {
	r := testrepo.New(t)
	src := t.TempDir() // no inputs, no script
	t.Chdir(r.Root)
	if err := runNew(context.Background(), []string{"first", "--from", src}); err == nil {
		t.Fatal("a starting point with nothing in it was accepted")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "runs", "first")); !os.IsNotExist(err) {
		t.Errorf("runs/first is left behind after the refusal")
	}
}
