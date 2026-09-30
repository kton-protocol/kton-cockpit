package github_test

import (
	"encoding/json"
	"os/exec"
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
