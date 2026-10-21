package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PsN's vpc writes psn.mod and runs nmfe on it: the nmfe call is a call of its own, inside vpc,
// and psn.mod is its input although the task wrote it. Paths are keyed relative to the working
// directory, the licence outside the project is listed, not keyed, and the same call made from
// another checkout gets the same key.
func TestCalls_NestedToolCallsAndKeys(t *testing.T) {
	mk := func(root string) *Task {
		return &Task{ID: "16", Session: "claude-x",
			Parents: map[int]int{21: 20, 22: 21},
			Commands: []Command{
				{PID: 20, PPID: 1, Cmdline: "perl /usr/local/bin/vpc run6.mod -samples=10", Cwd: root + "/models"},
				{PID: 22, PPID: 21, Cmdline: "/bin/bash /opt/nm/run/nmfe75 psn.mod psn.lst -licfile=/opt/nm/license/nonmem.lic", Cwd: root + "/models/vpc/NM_run1"},
			},
			Ops: []FileOp{
				{PID: 20, Op: "r", Path: root + "/models/run6.mod", SHA256: "m6"},
				{PID: 20, Op: "r", Path: root + "/data/pk.csv", SHA256: "d"},
				{PID: 21, Op: "w", Path: root + "/models/vpc/NM_run1/psn.mod"},
				{PID: 21, Op: "w", Path: root + "/models/vpc/NM_run1/psn.mod"},
				{PID: 21, Op: "c", Path: root + "/models/vpc/NM_run1/psn.mod", SHA256: "p"},
				{PID: 22, Op: "r", Path: root + "/models/vpc/NM_run1/psn.mod", SHA256: "p"},
				{PID: 22, Op: "r", Path: root + "/data/pk.csv", SHA256: "d"},
				{PID: 22, Op: "r", Path: "/opt/nm/license/nonmem.lic", SHA256: "lic", Outside: true},
				{PID: 22, Op: "w", Path: root + "/models/vpc/NM_run1/psn.ext"},
				{PID: 22, Op: "c", Path: root + "/models/vpc/NM_run1/psn.ext", SHA256: "e"},
				{PID: 20, Op: "r", Path: root + "/models/vpc/NM_run1/psn.ext", SHA256: "e"},
				{PID: 20, Op: "w", Path: root + "/models/vpc/vpc_results.csv"},
				{PID: 20, Op: "c", Path: root + "/models/vpc/vpc_results.csv", SHA256: "v"},
			}}
	}
	cs := CallsOf(mk("/home/a/proj"))
	if len(cs) != 2 || cs[0].Tool != "vpc run6.mod -samples=10" || cs[1].Parent != 20 {
		t.Fatalf("calls %+v", cs)
	}
	vpc, nm := cs[0], cs[1]
	if len(vpc.Reads) != 2 || vpc.Reads[1].Rel != "../data/pk.csv" { // psn.mod and psn.ext are its own
		t.Fatalf("vpc reads %+v", vpc.Reads)
	}
	if len(nm.Reads) != 2 || nm.Reads[0].Rel != "psn.mod" || len(nm.Outside) != 1 || len(nm.Writes) != 1 {
		t.Fatalf("nmfe %+v", nm)
	}
	if nm.Argv[0] != "nmfe75" || nm.Argv[3] != "-licfile=nonmem.lic" {
		t.Fatalf("argv %v", nm.Argv)
	}
	other := CallsOf(mk("/srv/b/checkout"))
	if other[1].Key != nm.Key || other[0].Key != vpc.Key {
		t.Fatalf("the same call in another checkout must have the same key")
	}
	changed := mk("/home/a/proj")
	changed.Ops[5].SHA256 = "d2" // nmfe reads other data
	if CallsOf(changed)[1].Key == nm.Key {
		t.Fatalf("other input, same key")
	}
}

func TestCalls_WorkFilesAndUnchangedWritesAreNotData(t *testing.T) {
	tk := &Task{ID: "9", Commands: []Command{{PID: 5, PPID: 1, Cmdline: "/bin/bash /x/nmfe75 run1.mod run1.lst", Cwd: "/p/models"}},
		Ops: []FileOp{
			{PID: 5, Op: "r", Path: "/p/models/run1.mod", SHA256: "m"},
			{PID: 5, Op: "r", Path: "/p/models/prsame.set", SHA256: "s"}, // left by the previous run
			{PID: 5, Op: "r", Path: "/p/models/FDATA.csv", SHA256: "fd"},
			{PID: 5, Op: "r", Path: "/p/data/pk.csv", SHA256: "d"},
			{PID: 5, Op: "w", Path: "/p/data/pk.csv"},
			{PID: 5, Op: "c", Path: "/p/data/pk.csv", SHA256: "d"}, // read-write, unchanged
			{PID: 5, Op: "c", Path: "/p/models/temp_dir/PRSIZES.f90", SHA256: "f"},
			{PID: 5, Op: "c", Path: "/p/models/FCON (deleted)", SHA256: "x"},
			{PID: 5, Op: "w", Path: "/p/models/run1.ext"},
			{PID: 5, Op: "c", Path: "/p/models/run1.ext", SHA256: "e"},
		}}
	c := CallsOf(tk)[0]
	if len(c.Reads) != 2 || len(c.Writes) != 1 || c.Writes[0].Rel != "run1.ext" {
		t.Fatalf("reads %+v writes %+v", c.Reads, c.Writes)
	}
}

// The CAS keeps a file only with the bytes it was observed with.
func TestCAS_KeepsOnlyTheObservedBytes(t *testing.T) {
	dir := t.TempDir()
	c := &CAS{Dir: filepath.Join(dir, "cas")}
	f := filepath.Join(dir, "run1.ext")
	os.WriteFile(f, []byte("OFV 123"), 0o644)
	sum := "ab2f7ee1e7c4f9f3a1d6ea1a8b5c1f0f5e0b8f4e3b2f8b4f4e1b1e0f7c9a6d21" // not the hash of these bytes
	if ok, err := c.Put(f, sum); ok || err != nil || c.Has(sum) {
		t.Fatalf("kept bytes under a hash they do not have: %v %v", ok, err)
	}
	h := sha256Hex([]byte("OFV 123"))
	if ok, err := c.Put(f, h); !ok || err != nil || !c.Has(h) {
		t.Fatalf("not kept: %v %v", ok, err)
	}
	if b, _ := os.ReadFile(c.Path(h)); string(b) != "OFV 123" {
		t.Fatalf("content %q", b)
	}
}

// raw data kept → script → derived (gone) → nmfe → ext (gone): ext can be made again.
// A version whose producer read something lost is lost too, and says which input.
func TestProducible_FollowsCallsBackToKeptBytes(t *testing.T) {
	f := func(rel, sum string) CallFile { return CallFile{Path: "/p/" + rel, Rel: rel, SHA256: sum} }
	calls := []Call{
		{Task: "1", Tool: "Rscript R/01.R", Reads: []CallFile{f("raw.csv", "raw")}, Writes: []CallFile{f("derived.csv", "der")}},
		{Task: "2", Tool: "nmfe75 run1.mod", Reads: []CallFile{f("run1.mod", "mod"), f("derived.csv", "der")}, Writes: []CallFile{f("run1.ext", "ext")}},
		{Task: "3", Tool: "Rscript R/02.R", Reads: []CallFile{f("edited.csv", "gone")}, Writes: []CallFile{f("plot.png", "png")}},
	}
	kept := map[string]bool{"raw": true, "mod": true}
	got := map[string]Producible{}
	for _, p := range producibleOf(calls, func(s string) bool { return kept[s] }) {
		got[p.SHA256] = p
	}
	if got["raw"].State != "kept" || got["der"].State != "producible" || got["ext"].State != "producible" {
		t.Fatalf("%+v", got)
	}
	if p := got["png"]; p.State != "lost" || len(p.Lost) != 1 || p.Lost[0] != "edited.csv" {
		t.Fatalf("%+v", p)
	}
}

// The plan reruns the call that made the file, takes kept inputs from the CAS, and reruns the
// call behind an input that is not kept.
func TestReproPlan_RerunsTheMakerAndWhatIsNotKept(t *testing.T) {
	f := func(rel, sum string) CallFile { return CallFile{Path: "/p/" + rel, Rel: rel, SHA256: sum} }
	calls := []Call{
		{Task: "1", PID: 1, Tool: "Rscript R/01.R", Cwd: "/p", Argv: []string{"Rscript", "R/01.R"},
			Reads: []CallFile{f("R/01.R", "s1"), f("raw.csv", "raw")}, Writes: []CallFile{f("derived.csv", "der")}},
		{Task: "2", PID: 2, Tool: "nmfe75 run1.mod run1.lst", Cwd: "/p/models", Argv: []string{"nmfe75", "run1.mod", "run1.lst"},
			Reads:  []CallFile{{Path: "/p/models/run1.mod", Rel: "run1.mod", SHA256: "mod"}, {Path: "/p/derived.csv", Rel: "../derived.csv", SHA256: "der"}},
			Writes: []CallFile{{Path: "/p/models/run1.ext", Rel: "run1.ext", SHA256: "ext"}}},
		{Task: "3", PID: 3, Tool: "Rscript R/02.R", Cwd: "/p", Argv: []string{"Rscript", "R/02.R"},
			Reads: []CallFile{f("R/02.R", "s2"), f("models/run1.ext", "ext")}, Writes: []CallFile{f("gof.png", "png")}},
	}
	kept := map[string]bool{"s1": true, "raw": true, "mod": true, "s2": true, "ext": true}
	p := reproPlanOf(calls, func(s string) bool { return kept[s] }, "/p", "/p/gof.png", "")
	if len(p.Run) != 1 || !p.Run[0].Target || len(p.Put) != 2 || len(p.Lost) != 0 {
		t.Fatalf("ext kept: only the plot reruns: %+v", p)
	}
	delete(kept, "ext") // the NONMEM output is gone: nmfe reruns, and derived.csv before it
	p = reproPlanOf(calls, func(s string) bool { return kept[s] }, "/p", "/p/gof.png", "")
	if len(p.Run) != 3 || p.Run[1].Cwd != "models" || !p.Run[2].Target || len(p.Lost) != 0 {
		t.Fatalf("ext gone: %+v", p)
	}
	delete(kept, "raw") // and the raw data: nothing can make derived.csv
	p = reproPlanOf(calls, func(s string) bool { return kept[s] }, "/p", "/p/gof.png", "")
	if len(p.Lost) != 1 || p.Lost[0] != "raw.csv" {
		t.Fatalf("raw gone: %+v", p)
	}
}

// PsN copies the data into its directory; the data's maker is the script that wrote it there.
func TestReproPlan_TheMakerIsTheFirstWriterAtThePath(t *testing.T) {
	calls := []Call{
		{Task: "8", PID: 1, Tool: "Rscript R/01.R", Cwd: "/p", Reads: []CallFile{{Path: "/p/R/01.R", Rel: "R/01.R", SHA256: "s"}},
			Writes: []CallFile{{Path: "/p/data/pk.csv", Rel: "data/pk.csv", SHA256: "d"}}},
		{Task: "16", PID: 2, Tool: "vpc run5.mod", Cwd: "/p/models", Reads: []CallFile{{Path: "/p/data/pk.csv", Rel: "../data/pk.csv", SHA256: "d"}},
			Writes: []CallFile{{Path: "/p/models/vpc/m1/pk.csv", Rel: "vpc/m1/pk.csv", SHA256: "d"}}},
	}
	p := reproPlanOf(calls, func(s string) bool { return s == "s" }, "/p", "/p/data/pk.csv", "")
	if p == nil || len(p.Run) != 1 || p.Run[0].Task != "8" {
		t.Fatalf("%+v", p)
	}
}

func TestCompare_NumbersWithinToleranceDatesIgnored(t *testing.T) {
	a := []byte("Fri Oct 16 09:20:01 CEST 2026\n ITERATION  OFV\n 17  1.54762E+00  121.46606655149152\n")
	b := []byte("Fri Oct 16 10:31:40 CEST 2026\n ITERATION  OFV\n 17  1.54762E+00  121.46606656138587\n")
	if c := CompareFiles(a, b, 1e-6); c.Verdict != "within" || c.Numbers != 1 || c.MaxRel > 1e-9 {
		t.Fatalf("%+v", c)
	}
	if c := CompareFiles(a, []byte(strings.Replace(string(b), "1.54762E+00", "1.60000E+00", 1)), 1e-3); c.Verdict != "different" {
		t.Fatalf("%+v", c)
	}
	if c := CompareFiles([]byte("ID,WT\n1,70\n"), []byte("ID,AGE\n1,70\n"), 1); c.Verdict != "different" {
		t.Fatalf("text must match: %+v", c)
	}
}

// What PsN deletes after NONMEM ran is NONMEM's output, not PsN's.
func TestCalls_DeletedLaterIsTheInnerCallsOutputOnly(t *testing.T) {
	tk := &Task{ID: "1", Parents: map[int]int{11: 10},
		Commands: []Command{{PID: 10, PPID: 1, Cmdline: "perl /b/vpc run.mod", Cwd: "/p/m"}, {PID: 11, PPID: 10, Cmdline: "bash /b/nmfe75 psn.mod psn.lst", Cwd: "/p/m/v/NM_run1"}},
		Ops: []FileOp{
			{PID: 10, Op: "w", Path: "/p/m/v/NM_run1/psn.mod"},
			{PID: 10, Op: "c", Path: "/p/m/v/NM_run1/psn.mod", SHA256: "pm"},
			{PID: 11, Op: "r", Path: "/p/m/v/NM_run1/psn.mod", SHA256: "pm"},
			{PID: 11, Op: "w", Path: "/p/m/v/NM_run1/psn.ext"},
			{PID: 11, Op: "c", Path: "/p/m/v/NM_run1/psn.ext", SHA256: "e"},
			{PID: 11, Op: "c", Path: "/p/m/vpc.log", SHA256: "log"}, // the caller's redirect, inherited
			{PID: 10, Op: "r", Path: "/p/m/v/NM_run1/psn.ext", SHA256: "e"},
			{PID: 10, Op: "w", Path: "/p/m/v/vpc_results.csv"},
			{PID: 10, Op: "c", Path: "/p/m/v/vpc_results.csv", SHA256: "res"},
			{PID: 10, Op: "d", Path: "/p/m/v/NM_run1/psn.ext"},
			{PID: 10, Op: "d", Path: "/p/m/v/NM_run1/psn.mod"},
		}}
	cs := CallsOf(tk)
	if len(cs[0].Writes) != 1 || cs[0].Writes[0].Rel != "v/vpc_results.csv" {
		t.Fatalf("vpc leaves only its results: %+v", cs[0].Writes)
	}
	if len(cs[1].Writes) != 1 || cs[1].Writes[0].Rel != "psn.ext" {
		t.Fatalf("nmfe keeps what it wrote: %+v", cs[1].Writes)
	}
}
