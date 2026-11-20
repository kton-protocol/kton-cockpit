package jobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A run's lines fold into its last state, its start and end; ticks and a torn line are left out; a
// writer's file past the bound is moved aside and still read.
func TestJournal_FoldsRunsAcrossWritersAndRotations(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	origin := json.RawMessage(`{"kind":"act","package":"dryrun1.runs","composition":"main","part":"p1","act":"rerun"}`)
	must(Append(dir, "cockpit", Line{ID: "run-1", Kind: KindWorkflow, Ray: &Ray{Package: "dryrun1.runs", Revision: "v7"}, Origin: origin, State: Queued}))
	must(Append(dir, "cockpit", Line{ID: "run-1", Kind: KindWorkflow, State: Running}))
	must(Append(dir, "runner", Line{Kind: KindTick, Job: "nightly", Outcome: "skipped"}))
	must(Append(dir, "runner", Line{ID: "run-2", Kind: KindPoll, State: Running}))
	// The cockpit's file grows past the bound: the next line goes to a fresh file.
	big := filepath.Join(dir, "cockpit.jsonl")
	f, err := os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0o600)
	must(err)
	_, err = f.WriteString(strings.Repeat(" ", RotateAt) + "\n{\"id\": \"torn\n")
	must(err)
	f.Close()
	must(Append(dir, "cockpit", Line{ID: "run-1", Kind: KindWorkflow, State: Succeeded, Steps: []string{"sha256:aa"}}))
	if _, err := os.Stat(filepath.Join(dir, "cockpit.1.jsonl")); err != nil {
		t.Fatalf("the full file is moved aside: %v", err)
	}

	runs, err := Runs(dir)
	must(err)
	by := map[string]Run{}
	for _, r := range runs {
		by[r.ID] = r
	}
	r1 := by["run-1"]
	if len(runs) != 2 || r1.State != Succeeded || r1.End == "" || r1.Start == "" || r1.Ray == nil || r1.Ray.Revision != "v7" ||
		len(r1.Steps) != 1 || r1.Writer != "cockpit" || !Matches(r1.Origin, json.RawMessage(`{"kind":"act","package":"dryrun1.runs"}`)) {
		t.Errorf("run-1 folds across the rotation: %+v", r1)
	}
	if r2 := by["run-2"]; r2.State != Running || r2.End != "" || r2.Writer != "runner" {
		t.Errorf("run-2, the runner's: %+v", r2)
	}
}

// An act filter is a prefix of package, composition, part, act; a subtree is one filter.
func TestJournal_AnActFilterIsAPrefix(t *testing.T) {
	o := json.RawMessage(`{"kind":"act","package":"p","composition":"c","part":"x","act":"a"}`)
	for f, want := range map[string]bool{
		``:                             true,
		`{"kind":"act","package":"p"}`: true,
		`{"kind":"act","package":"p","composition":"c"}`:            true,
		`{"kind":"act","package":"p","composition":"c","part":"y"}`: false,
		`{"kind":"schedule"}`: false,
	} {
		if got := Matches(o, json.RawMessage(f)); got != want {
			t.Errorf("filter %s: %v, want %v", f, got, want)
		}
	}
	if CheckActFilter(json.RawMessage(`{"kind":"act","part":"x"}`)) == nil {
		t.Error("a part without package and composition is no prefix")
	}
	if err := CheckActFilter(json.RawMessage(`{"kind":"act","package":"p","composition":"c"}`)); err != nil {
		t.Errorf("a prefix: %v", err)
	}
}
