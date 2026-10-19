package github_test

import (
	"context"
	"encoding/json"
	"os"
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

// Sync takes in what another clone pushed and shares what is here. When both moved it merges —
// keeping the local commit, which records' locators pin — and refuses on a conflict.
func TestSync_TakesInTheirsKeepsOursAndRefusesAConflict(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	repo := r.Config(t).Repo
	// The fetch URL names github.com, which the guard reads; pulling has to reach the bare origin.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+r.Origin+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "git@github.com:"+testrepo.Owner+"/"+testrepo.Name+".git")
	b := github.Backend{}
	gitc := []string{"-c", "user.name=x", "-c", "user.email=x@x"}
	commit := func(dir, file, content, msg string) string {
		must(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
		run(t, dir, "git", "add", file)
		run(t, dir, "git", append(gitc, "commit", "--quiet", "-m", msg)...)
		return strings.TrimSpace(run(t, dir, "git", "rev-parse", "HEAD"))
	}

	r.Write(t, "a.txt", "1\n")
	if _, err := b.Persist(ctx, repo, []string{"a.txt"}, "a"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	run(t, "", "git", "clone", "--quiet", r.Origin, other)

	// Only they moved: a fast-forward.
	theirs := commit(other, "b.txt", "b\n", "theirs")
	run(t, other, "git", "push", "--quiet", "origin", "HEAD")
	got, err := b.Sync(ctx, repo)
	if err != nil || got.ID != theirs {
		t.Fatalf("after Sync, head is %s (%v), want %s", got.ID, err, theirs)
	}

	// Both moved, on different files: theirs comes in, ours stays, and both reach origin.
	mine := commit(r.Root, "c.txt", "c\n", "mine")
	commit(other, "d.txt", "d\n", "theirs again")
	run(t, other, "git", "push", "--quiet", "origin", "HEAD")
	got, err = b.Sync(ctx, repo)
	if err != nil || got.Rejected {
		t.Fatalf("Sync: %v %s", err, got.Reason)
	}
	run(t, r.Root, "git", "merge-base", "--is-ancestor", mine, "HEAD") // ours kept, not rewritten
	if remote := strings.TrimSpace(run(t, "", "git", "--git-dir", r.Origin, "rev-parse", "main")); remote != got.ID {
		t.Fatalf("Sync did not share: origin is at %s, here %s", remote, got.ID)
	}

	// Both changed the same file: refused, and nothing here moved.
	run(t, other, "git", "pull", "--quiet", "--no-rebase")
	mine = commit(r.Root, "a.txt", "mine\n", "conflict mine")
	commit(other, "a.txt", "theirs\n", "conflict theirs")
	run(t, other, "git", "push", "--quiet", "origin", "HEAD")
	if _, err := b.Sync(ctx, repo); err == nil {
		t.Fatal("Sync resolved a conflict on its own")
	}
	if head, _ := b.Head(ctx, repo); head.ID != mine {
		t.Fatalf("a refused Sync moved HEAD to %s", head.ID)
	}
	if st := run(t, r.Root, "git", "status", "--porcelain"); st != "" {
		t.Fatalf("a refused Sync left the working tree changed:\n%s", st)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
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
