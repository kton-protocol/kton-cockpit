package github_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/cockpit/backend/backendtest"
	"github.com/kton-protocol/kton-cockpit/cockpit/backend/github"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

type fixture struct{ repos map[string]*testrepo.Repo }

func (f *fixture) Bound(t *testing.T) backend.Repo {
	r := testrepo.New(t)
	f.repos[r.Root] = r
	return r.Config(t).Repo
}

// Elsewhere is the same repository with a configuration that names another GitHub repository —
// what git mode's guard is there to catch.
func (f *fixture) Elsewhere(t *testing.T) backend.Repo {
	repo := f.Bound(t)
	var c github.Config
	if err := repo.Decode(&c); err != nil {
		t.Fatal(err)
	}
	c.Name = "someone-elses-repo"
	b, _ := json.Marshal(c)
	repo.Block = b
	return repo
}

func (f *fixture) Write(t *testing.T, r backend.Repo, path, content string) {
	f.repos[r.Dir].Write(t, path, content)
}

// Fetch reads the bytes a permalink names from the bare repository pushes land in: a permalink is
// https://raw.githubusercontent.com/<owner>/<name>/<sha>/<path>.
func (f *fixture) Fetch(t *testing.T, r backend.Repo, uri string) ([]byte, bool) {
	rest := strings.TrimPrefix(uri, "https://raw.githubusercontent.com/")
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) != 4 {
		t.Fatalf("not a permalink: %s", uri)
	}
	out, err := exec.Command("git", "--git-dir", f.repos[r.Dir].Origin, "show", parts[2]+":"+parts[3]).Output()
	if err != nil {
		t.Fatalf("the commit a permalink pins is not on the remote: %v", err)
	}
	return out, true
}

func TestConformance(t *testing.T) {
	backendtest.Run(t, github.Backend{}, &fixture{repos: map[string]*testrepo.Repo{}})
}

// Sync takes in what another clone pushed, and refuses — never merges — once the two have diverged.
func TestSync_FastForwardsAndRefusesADivergedHistory(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	repo := r.Config(t).Repo
	// The fetch URL names github.com, which the guard reads; pulling has to reach the bare origin.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+r.Origin+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "git@github.com:"+testrepo.Owner+"/"+testrepo.Name+".git")
	b := github.Backend{}

	r.Write(t, "a.txt", "1\n")
	if _, err := b.Persist(ctx, repo, []string{"a.txt"}, "a"); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(t.TempDir(), "other")
	run(t, "", "git", "clone", "--quiet", r.Origin, other)
	run(t, other, "git", "-c", "user.name=o", "-c", "user.email=o@x", "commit", "--quiet", "--allow-empty", "-m", "theirs")
	run(t, other, "git", "push", "--quiet", "origin", "HEAD")
	theirs := strings.TrimSpace(run(t, other, "git", "rev-parse", "HEAD"))

	got, err := b.Sync(ctx, repo)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got.ID != theirs {
		t.Fatalf("after Sync, head is %s, want %s", got.ID, theirs)
	}

	// Both sides move on: Sync must refuse, and leave the local commit where it is.
	run(t, r.Root, "git", "-c", "user.name=m", "-c", "user.email=m@x", "commit", "--quiet", "--allow-empty", "-m", "mine")
	mine := strings.TrimSpace(run(t, r.Root, "git", "rev-parse", "HEAD"))
	run(t, other, "git", "-c", "user.name=o", "-c", "user.email=o@x", "commit", "--quiet", "--allow-empty", "-m", "theirs again")
	run(t, other, "git", "push", "--quiet", "origin", "HEAD")
	if _, err := b.Sync(ctx, repo); err == nil {
		t.Fatal("Sync merged a diverged history")
	}
	if head, _ := b.Head(ctx, repo); head.ID != mine {
		t.Fatalf("a refused Sync moved HEAD to %s", head.ID)
	}
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
