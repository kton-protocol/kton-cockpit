package oxigraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// A build of a view says so while it runs: a marker under the index's directory, one per building
// process, with the slot, the repository, when it began and what it is doing. A frontend that waits on an
// ask reads it (cockpit index status --json) instead of guessing from timeouts (userinterface, T12). A
// marker whose process is gone is no build: it is ignored and removed.

// Building is one build of a view that is running.
type Building struct {
	Slot  string    `json:"slot"`
	Repo  string    `json:"repo"`
	Since time.Time `json:"since"`
	Phase string    `json:"phase"` // reading the registries | projecting the claims | writing the view
	PID   int       `json:"pid"`
}

type buildMarker struct {
	path string
	b    Building
}

func buildingDir(indexDir string) string { return filepath.Join(indexDir, "building") }

// markBuilding records that this process builds slot for repo; done removes it.
func markBuilding(indexDir, slot, repo string) *buildMarker {
	m := &buildMarker{path: filepath.Join(buildingDir(indexDir), fmt.Sprintf("%s-%d.json", slot, os.Getpid())),
		b: Building{Slot: slot, Repo: repo, Since: time.Now().UTC().Truncate(time.Second), PID: os.Getpid()}}
	_ = os.MkdirAll(buildingDir(indexDir), 0o755)
	m.phase("reading the registries")
	return m
}

func (m *buildMarker) phase(p string) {
	m.b.Phase = p
	b, _ := json.Marshal(m.b)
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (m *buildMarker) done() { _ = os.Remove(m.path) }

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
		if !alive(b.PID) {
			_ = os.Remove(p) // a killed build: no build
			continue
		}
		if repo == "" || b.Repo == repo {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
