//go:build docker

package cli

import (
	"encoding/json"
	"testing"
)

// Over serve, workflow/run answers the run's id at once; the run is in the checkout's jobs journal with
// its origin, runStarted and runFinished follow, run/status says how it ended and with which steps,
// run/list finds it by a prefix of its act origin; a run refused before it ran ends failed, not
// queued (SPEC §12.5; userinterface's job monitor, MH 21.11.).
func TestServe_AWorkflowRunIsAJobWithItsOrigin(t *testing.T) {
	r := verticleRepo(t, alpineImage, alpineImage)
	c := startServe(t)
	c.ok("initialize", map[string]any{"root": r.Root, "events": map[string]any{"intervalMs": 500}})
	origin := map[string]string{"kind": "act", "package": "qa", "composition": "main", "part": "p1", "act": "greet"}
	var started struct {
		RunID string `json:"runId"`
	}
	must(t, json.Unmarshal(c.ok("workflow/run", map[string]any{"name": "greet",
		"bindings": map[string]string{"name": "data/ada.txt"}, "origin": origin}), &started))
	if started.RunID == "" {
		t.Fatal("workflow/run answers the run's id")
	}
	got := c.waitEvent("events/runFinished")
	sawStart := false
	for _, e := range got {
		sawStart = sawStart || e.Method == "events/runStarted"
	}
	var fin struct {
		ID, State string
		Origin    map[string]string
	}
	must(t, json.Unmarshal(got[len(got)-1].Params, &fin))
	if !sawStart || fin.ID != started.RunID || fin.State != "succeeded" || fin.Origin["part"] != "p1" {
		t.Errorf("runStarted then runFinished succeeded with the origin: %+v (events %v)", fin, got)
	}
	var st struct {
		State string   `json:"state"`
		Steps []string `json:"steps"`
		Ray   struct{ Package string }
		End   string `json:"end"`
	}
	must(t, json.Unmarshal(c.ok("run/status", map[string]any{"id": started.RunID}), &st))
	if st.State != "succeeded" || len(st.Steps) != 1 || st.Ray.Package != "greet" || st.End == "" {
		t.Errorf("run/status: %+v", st)
	}
	count := func(filter map[string]string) int {
		var l struct {
			Runs []struct{ ID string } `json:"runs"`
		}
		must(t, json.Unmarshal(c.ok("run/list", map[string]any{"origin": filter}), &l))
		return len(l.Runs)
	}
	if n := count(map[string]string{"kind": "act", "package": "qa", "composition": "main"}); n != 1 {
		t.Errorf("run/list by a prefix of the origin: want the run, got %d", n)
	}
	if n := count(map[string]string{"kind": "act", "package": "qa", "composition": "other"}); n != 0 {
		t.Errorf("run/list by another composition: want none, got %d", n)
	}
	if rep := c.call("run/list", map[string]any{"origin": map[string]string{"kind": "act", "part": "p1"}}); rep.refusalCode() != "argument" {
		t.Errorf("a part without its package and composition is no prefix: %+v", rep)
	}

	// Refused before it ran (a required hole unbound): failed, with the reason.
	must(t, json.Unmarshal(c.ok("workflow/run", map[string]any{"name": "greet", "origin": origin}), &started))
	got = c.waitEvent("events/runFinished")
	must(t, json.Unmarshal(got[len(got)-1].Params, &fin))
	if fin.ID != started.RunID || fin.State != "failed" {
		t.Errorf("a run refused before it ran: want failed, got %+v", fin)
	}
}
