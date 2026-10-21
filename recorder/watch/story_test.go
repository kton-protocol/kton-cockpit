package watch

import (
	"strings"
	"testing"
)

func TestStory_TellsThePathAndTheDetours(t *testing.T) {
	st := storyOf(workday(), "/p", "/p/fuer_sie.csv", "")
	if st == nil {
		t.Fatal("no story")
	}
	var on, off int
	for _, s := range st.Steps {
		if s.OnPath {
			on++
		} else {
			off++
		}
	}
	if on != 6 || off != 2 { // the peek and the failed read are told, as detours
		t.Fatalf("on %d off %d: %+v", on, off, st.Steps)
	}
	first := st.Steps[0]
	if !strings.Contains(first.Sentence, "liest du „data.csv“") || !strings.Contains(first.Sentence, "legst") {
		t.Fatalf("sentence %q", first.Sentence)
	}
	var merge StoryStep
	for _, s := range st.Steps {
		if s.Session == "s2" && s.Task == "2" {
			merge = s
		}
	}
	if !strings.Contains(merge.Sentence, "arbeitest du mit „c2“ und „lab“") {
		t.Fatalf("merge sentence %q", merge.Sentence)
	}
	for _, s := range st.Steps {
		if s.Error && !strings.HasSuffix(s.Sentence, "Das scheitert.") {
			t.Fatalf("error step %q", s.Sentence)
		}
	}
}

// The story starts before a hand edit: the session that wrote the edited file comes first,
// then the edit itself as a step of its own, then what was done with it.
func TestStory_StartsBeforeTheHandEdit(t *testing.T) {
	ts := []*Task{
		{Session: "a", ID: "1", Start: 1, End: 2, Expr: `write.csv(x, "a.csv")`, Script: "/p/01.R",
			Outputs: []*Output{{Path: "/p/a.csv", SHA256: "v1aaaaaaaaaaaa", Class: ClassProject, Now: NowChanged}}},
		{Session: "b", ID: "1", Start: 10, End: 11, Expr: `y <- read.csv("a.csv")`, Script: "/p/02.R", Vars: []Var{{Name: "y"}},
			Inputs: []*Input{{Path: "/p/a.csv", SHA256: "v2bbbbbbbbbbbb", Class: ClassProject, Status: "changed-outside", Now: NowChanged}}},
		{Session: "b", ID: "2", Start: 11, End: 12, Expr: `write.csv(y, "out.csv")`, Script: "/p/02.R",
			Uses:    []VarUse{{Name: "y", Task: "1"}},
			Outputs: []*Output{{Path: "/p/out.csv", SHA256: "o", Class: ClassProject}}},
	}
	st := storyOf(ts, "/p", "/p/out.csv", "")
	var got []string
	for _, s := range st.Steps {
		got = append(got, s.Session+"/"+s.Task)
	}
	if strings.Join(got, " ") != "a/1 hand/a.csv@v2bbbbbbbbbb b/1 b/2" {
		t.Fatalf("steps %v", got)
	}
	if !st.Steps[0].OnPath || !st.Steps[1].Hand || !strings.Contains(st.Steps[1].Sentence, "von Hand") {
		t.Fatalf("%+v", st.Steps[:2])
	}
}

func TestStory_ShellCommandIsThePlace(t *testing.T) {
	for in, want := range map[string]string{
		"Rscript R/01_data_qc.R && Rscript R/02_eda.R && head -5 data/x.csv": "Rscript R/01_data_qc.R",
		"cd /p/models && nmfe75 run001.mod run001.lst > run001.log 2>&1":     "nmfe75 run001.mod run001.lst > run001.log 2>&1",
		"cd /p/models # run002: allometrisch\nsed -e 's/a/b/' run001.mod":    "sed -e 's/a/b/' run001.mod",
		"ls -la figures/ | head": "ls -la figures/",
	} {
		if got := shellCommand(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if g := collapse([]string{"a", "b", "a", "a"}); strings.Join(g, "|") != "a (3×)|b" {
		t.Fatalf("collapse %v", g)
	}
}

func TestStory_ShellPlaceNamesTheOutermostTools(t *testing.T) {
	tk := &Task{Session: "claude-x", Expr: "sed -i 's/a/b/' run2.mod && cd models && nmfe75 run2.mod run2.lst", Commands: []Command{
		{PID: 10, PPID: 1, Program: "/usr/bin/sed", Cmdline: "sed -i s/a/b/ run2.mod"},
		{PID: 11, PPID: 1, Program: "/bin/bash", Cmdline: "/bin/bash /home/u/.local/bin/nmfe75 run2.mod run2.lst"},
		{PID: 12, PPID: 11, Program: "/opt/nm/nonmem", Cmdline: "/opt/nm/nonmem run2.mod"},
		{PID: 20, PPID: 1, Program: "/usr/bin/perl", Cmdline: "perl /home/u/bin/vpc run2.mod -samples=10"},
		{PID: 21, PPID: 20, Program: "/bin/bash", Cmdline: "/bin/bash /opt/nm/run/nmfe75 psn.mod psn.lst"},
		{PID: 22, PPID: 20, Program: "/usr/bin/Rscript", Cmdline: "Rscript -e rmarkdown::render(x)"},
	}}
	if got := shellPlace(tk); got != "nmfe75 run2.mod run2.lst · vpc run2.mod -samples=10" {
		t.Fatalf("%q", got)
	}
	if got := shellPlace(&Task{Session: "claude-x", Expr: "ls -la | head"}); got != "ls -la" {
		t.Fatalf("%q", got)
	}
}

// PsN forks perl without exec: the chain goes through a process that only shows in the log
// lines' parent column.
func TestStory_ShellPlaceFollowsForks(t *testing.T) {
	tk := &Task{Session: "claude-x", Parents: map[int]int{30: 20, 31: 30}, Commands: []Command{
		{PID: 20, PPID: 1, Cmdline: "perl /home/u/bin/vpc run6.mod"},
		{PID: 31, PPID: 30, Cmdline: "/bin/bash /opt/nm/run/nmfe75 psn.mod psn.lst"},
	}}
	if got := shellPlace(tk); got != "vpc run6.mod" {
		t.Fatalf("%q", got)
	}
}
