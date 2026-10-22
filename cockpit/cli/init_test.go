package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// init asks the backends how to bind the folder: a git working tree with a GitHub origin becomes
// git mode with that owner and name; a folder no backend recognises becomes local mode, bound to
// itself (ADR-004).
func TestInit_TheBackendThatRecognisesTheFolderBindsIt(t *testing.T) {
	read := func(dir string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "cockpit.config.json"))
		must(t, err)
		var c struct {
			Repo map[string]any `json:"repo"`
		}
		must(t, json.Unmarshal(b, &c))
		return c.Repo
	}

	gitDir := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", "git@github.com:someone/somewhere.git"}} {
		c := exec.Command("git", args...)
		c.Dir = gitDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(gitDir)
	must(t, runInit(context.Background()))
	if repo := read(gitDir); repo["owner"] != "someone" || repo["name"] != "somewhere" || repo["mode"] != nil {
		t.Errorf("a GitHub working tree was not bound in git mode: %v", repo)
	}

	plain := t.TempDir()
	t.Chdir(plain)
	must(t, runInit(context.Background()))
	if repo := read(plain); repo["mode"] != "local" || repo["root"] != plain {
		t.Errorf("a plain folder was not bound in local mode to itself: %v", repo)
	}
}
