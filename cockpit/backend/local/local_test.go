package local_test

import (
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/cockpit/backend/backendtest"
	"github.com/kton-protocol/kton-cockpit/cockpit/backend/local"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

type fixture struct{ repos map[string]*testrepo.Repo }

func (f *fixture) Bound(t *testing.T) backend.Repo {
	r := testrepo.NewLocal(t)
	f.repos[r.Root] = r
	return r.Config(t).Repo
}

// Elsewhere is the configuration copied to another directory: it still names the path it was
// written for, which is what local mode's guard compares.
func (f *fixture) Elsewhere(t *testing.T) backend.Repo {
	repo := f.Bound(t)
	repo.Dir = t.TempDir()
	return repo
}

func (f *fixture) Write(t *testing.T, r backend.Repo, path, content string) {
	f.repos[r.Dir].Write(t, path, content)
}

func (f *fixture) Fetch(*testing.T, backend.Repo, string) ([]byte, bool) { return nil, false }

func TestConformance(t *testing.T) {
	backendtest.Run(t, local.Backend{}, &fixture{repos: map[string]*testrepo.Repo{}})
}
