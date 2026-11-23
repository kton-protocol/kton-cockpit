package oxigraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/flock"
)

// A build of a view says so while it runs: a marker under the index's directory, one per building
// process, with the slot, the repository, when it began and what it is doing. A frontend that waits on an
// ask reads it (cockpit index status --json) instead of guessing from timeouts (userinterface, T12). The
// build holds a lock beside its marker for as long as it runs; a marker whose lock nobody holds is a
// killed build's: it is ignored and removed. No process id decides it: a killed build's id may belong
// to another process by now (kton-review B1 on #238).

// Building is one build of a view that is running.
type Building struct {
	Slot  string    `json:"slot"`
	Repo  string    `json:"repo"`
	Since time.Time `json:"since"`
	Phase string    `json:"phase"` // reading the registries | projecting the claims | writing the view
	PID   int       `json:"pid"`
}

type buildMarker struct {
	path   string
	b      Building
	unlock func()
}

func buildingDir(indexDir string) string { return filepath.Join(indexDir, "building") }

// markBuilding records that this process builds slot for repo; done removes it.
func markBuilding(indexDir, slot, repo string) *buildMarker {
	m := &buildMarker{path: filepath.Join(buildingDir(indexDir), fmt.Sprintf("%s-%d.json", slot, os.Getpid())),
		b: Building{Slot: slot, Repo: repo, Since: time.Now().UTC().Truncate(time.Second), PID: os.Getpid()}}
	_ = os.MkdirAll(buildingDir(indexDir), 0o755)
	if unlock, err := flock.Lock(lockOf(m.path)); err == nil {
		m.unlock = unlock
	}
	m.phase("reading the registries")
	return m
}

func lockOf(marker string) string { return strings.TrimSuffix(marker, ".json") + ".lock" }

func (m *buildMarker) phase(p string) {
	m.b.Phase = p
	b, _ := json.Marshal(m.b)
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (m *buildMarker) done() {
	_ = os.Remove(m.path)
	if m.unlock != nil {
		_ = os.Remove(lockOf(m.path))
		m.unlock()
	}
}

// BuildsRunning are the builds running under indexDir for repo ("" for every repository), oldest first.
func BuildsRunning(indexDir, repo string) []Building {
	entries, _ := os.ReadDir(buildingDir(indexDir))
	var out []Building
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(buildingDir(indexDir), e.Name())
		raw, err := os.ReadFile(p)
		var b Building
		if err != nil || json.Unmarshal(raw, &b) != nil {
			continue
		}
		unlock, held, err := flock.TryLock(lockOf(p))
		if err != nil {
			continue
		}
		if !held {
			// nobody holds it: a killed build's marker, no build. Its lock file stays: a new build of the
			// same name may already wait on it, and unlinking it would leave that build locking a file no
			// status finds (kton-review's nit on #241); sweepLocks removes it once it is old.
			_ = os.Remove(p)
			unlock()
			continue
		}
		if repo == "" || b.Repo == repo {
			out = append(out, b)
		}
	}
	sweepLocks(indexDir)
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// sweepLocks removes a lock file that has no marker beside it and has not changed for an hour: a killed
// build's, long past any build of its name.
func sweepLocks(indexDir string) {
	entries, _ := os.ReadDir(buildingDir(indexDir))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		p := filepath.Join(buildingDir(indexDir), e.Name())
		if _, err := os.Stat(strings.TrimSuffix(p, ".lock") + ".json"); err == nil {
			continue
		}
		if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > time.Hour {
			_ = os.Remove(p)
		}
	}
}
