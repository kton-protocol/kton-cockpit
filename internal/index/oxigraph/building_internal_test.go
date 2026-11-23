package oxigraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A build marks itself while it runs, per repository, and its marker goes when it ends; a marker whose
// process is gone is no build, and is removed (userinterface, T12).
func TestBuilding_AMarkerWhileABuildRuns(t *testing.T) {
	dir := t.TempDir()
	m := markBuilding(dir, "slot1", "/repo/a")
	m.phase("writing the view")
	got := BuildsRunning(dir, "/repo/a")
	if len(got) != 1 || got[0].Slot != "slot1" || got[0].Phase != "writing the view" || got[0].PID != os.Getpid() || time.Since(got[0].Since) > time.Minute {
		t.Fatalf("a running build: %+v", got)
	}
	if other := BuildsRunning(dir, "/repo/b"); len(other) != 0 {
		t.Errorf("another repository's status names no build of this one: %+v", other)
	}
	m.done()
	if got := BuildsRunning(dir, "/repo/a"); len(got) != 0 {
		t.Errorf("an ended build: %+v", got)
	}
	// A killed build left its marker, and its process id now belongs to a living process (pid 1, or
	// this test's): nobody holds its lock, so it is no build, and the marker is removed (kton-review B1 on
	// #238).
	for _, pid := range []int{1, os.Getpid()} {
		dead := filepath.Join(buildingDir(dir), "slot2-"+strconv.Itoa(pid)+".json")
		b, _ := json.Marshal(Building{Slot: "slot2", Repo: "/repo/a", Since: time.Now().Add(-3 * time.Hour), PID: pid, Phase: "projecting the claims"})
		if err := os.WriteFile(dead, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := BuildsRunning(dir, "/repo/a"); len(got) != 0 {
			t.Errorf("a killed build's marker, pid %d alive: no build, got %+v", pid, got)
		}
		if _, err := os.Stat(dead); !os.IsNotExist(err) {
			t.Errorf("the dead marker is removed: %v", err)
		}
	}
}
