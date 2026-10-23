package watch

import (
	"strings"
	"testing"
	"time"
)

// Someone sends data.csv. Morning: play around (a dead end), afternoon: the real analysis
// through objects, the deliverable at the end.
func workday() []*Task {
	ts := []*Task{
		{Session: "s1", ID: "1", Start: 1, End: 2, Expr: `d <- read.csv("data.csv")`, Vars: []Var{{Name: "d"}},
			Inputs: []*Input{{Path: "/p/data.csv", SHA256: "dat", Class: ClassProject, Now: NowSame}}},
		{Session: "s1", ID: "2", Start: 2, End: 3, Expr: `plot(d); write.csv(head(d), "peek.csv")`,
			Outputs: []*Output{{Path: "/p/peek.csv", SHA256: "peek", Class: ClassProject, Now: NowSame}}},
		{Session: "s1", ID: "3", Start: 3, End: 4, Expr: `clean <- subset(d, x > 0)`, Vars: []Var{{Name: "clean"}}},
		{Session: "s1", ID: "4", Start: 4, End: 5, Expr: `write.csv(clean, "clean.csv")`,
			Outputs: []*Output{{Path: "/p/clean.csv", SHA256: "cln", Class: ClassProject, Now: NowSame}}},
		{Session: "s2", ID: "1", Start: 10, End: 11, Expr: `c2 <- read.csv("clean.csv"); lab <- read.csv("/share/lab.csv")`, Vars: []Var{{Name: "c2"}, {Name: "lab"}},
			Inputs: []*Input{{Path: "/p/clean.csv", SHA256: "cln", Class: ClassProject, Now: NowSame, From: []Ref{{Session: "s1", Task: "4", Path: "/p/clean.csv"}}},
				{Path: "/share/lab.csv", SHA256: "lab", Class: ClassOutside, Now: NowSame}}},
		{Session: "s2", ID: "2", Start: 11, End: 12, Expr: `m <- merge(c2, lab)`, Vars: []Var{{Name: "m"}}},
		{Session: "s2", ID: "3", Start: 12, End: 13, Expr: `try(read.csv("x.cvs"))`, Status: "err"},
		{Session: "s2", ID: "4", Start: 13, End: 14, Expr: `write.csv(m, "fuer_sie.csv")`,
			Outputs: []*Output{{Path: "/p/fuer_sie.csv", SHA256: "out", Class: ClassProject, Now: NowSame}}},
	}
	for _, s := range []string{"s1", "s2"} {
		var sess []*Task
		for _, t := range ts {
			if t.Session == s {
				sess = append(sess, t)
			}
		}
		LinkVars(sess)
	}
	return ts
}

func TestRecipe_OnlyTheWayToTheFile(t *testing.T) {
	r := recipeOf(workday(), "/p", "/p/fuer_sie.csv", "")
	var ids []string
	for _, s := range r.Steps {
		ids = append(ids, s.Session+"#"+s.ID)
	}
	if strings.Join(ids, " ") != "s1#1 s1#3 s1#4 s2#1 s2#2 s2#4" {
		t.Fatalf("steps %v: the peek and the typo are detours", ids)
	}
	if r.Detours != 1 { // peek.csv (same session); the failed read has no effect
		t.Fatalf("detours %d", r.Detours)
	}
	if len(r.Sources) != 2 || r.Sources[0].Path != "/p/data.csv" || r.Sources[1].Path != "/share/lab.csv" {
		t.Fatalf("sources %+v", r.Sources)
	}
	if len(r.Gaps) != 0 { // where a file lies is no gap: reproducing tells whether it matters
		t.Fatalf("gaps %v", r.Gaps)
	}
	for _, want := range []string{`setwd("/p")`, `clean <- subset(d, x > 0)`, `m <- merge(c2, lab)`, `write.csv(m, "fuer_sie.csv")`} {
		if !strings.Contains(r.Script, want) {
			t.Fatalf("script lacks %q:\n%s", want, r.Script)
		}
	}
	if strings.Contains(r.Script, "peek.csv") {
		t.Fatalf("script contains the detour:\n%s", r.Script)
	}
}

func TestDescendants_WhatBecameOfTheDataset(t *testing.T) {
	d := descendantsOf(workday(), "/p/data.csv", "")
	if len(d.Steps) != 7 { // everything but the failed read; the peek uses d too
		t.Fatalf("%d steps", len(d.Steps))
	}
	var res []string
	for _, f := range d.Results {
		res = append(res, f.Path)
	}
	if strings.Join(res, " ") != "/p/fuer_sie.csv /p/peek.csv" {
		t.Fatalf("results %v", res)
	}
}

func TestFiles_Roles(t *testing.T) {
	roles := map[string]string{}
	for _, f := range filesOf(workday()) {
		roles[f.Path] = f.Role
	}
	if roles["/p/data.csv"] != "source" || roles["/p/clean.csv"] != "intermediate" || roles["/p/fuer_sie.csv"] != "result" {
		t.Fatalf("%v", roles)
	}
}

func TestVerdict_IdealIsAKnownScript(t *testing.T) {
	ts := []*Task{
		{Session: "s", ID: "1", Start: 1, End: 2, Expr: `source("prep.R")`, Sources: []Source{{Path: "/p/prep.R", Class: ClassProject}},
			Inputs:  []*Input{{Path: "/p/prep.R", SHA256: "prep", Class: ClassProject, Now: NowSame}, {Path: "/p/raw.csv", SHA256: "raw", Class: ClassProject, Now: NowSame}},
			Outputs: []*Output{{Path: "/p/clean.csv", SHA256: "cln", Class: ClassProject, Now: NowSame}}},
		{Session: "s", ID: "2", Start: 2, End: 3, Expr: `system("Rscript R/model.R")`, Commands: []Command{{Program: "/usr/bin/Rscript", Cmdline: "/usr/lib/R/bin/exec/R --no-echo --file=R/model.R"}},
			Inputs:  []*Input{{Path: "/p/R/model.R", SHA256: "mdl", Class: ClassProject, Now: NowChanged}, {Path: "/p/clean.csv", SHA256: "cln", Class: ClassProject, Now: NowSame, From: []Ref{{Task: "1", Path: "/p/clean.csv"}}}},
			Outputs: []*Output{{Path: "/p/fit.rds", SHA256: "fit", Class: ClassProject, Now: NowSame}}},
	}
	ts[1].Commands[0].Program = "/usr/bin/Rscript"
	v := recipeOf(ts, "/p", "/p/fit.rds", "").Verdict
	if v.Kind != "scripts" || v.Command != "Rscript prep.R && Rscript R/model.R" || len(v.Notes) != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestVerdict_ScriptThatNeedsTheConsole(t *testing.T) {
	ts := []*Task{
		{Session: "s", ID: "1", Start: 1, End: 2, Expr: `pk <- read.csv("pk.csv")`, Vars: []Var{{Name: "pk"}},
			Inputs: []*Input{{Path: "/p/pk.csv", SHA256: "pk", Class: ClassProject, Now: NowSame}}},
		{Session: "s", ID: "2", Start: 2, End: 3, Expr: `source("step2.R")`, Sources: []Source{{Path: "/p/step2.R", Class: ClassProject}},
			Uses:    []VarUse{{Name: "pk", Task: "1"}},
			Inputs:  []*Input{{Path: "/p/step2.R", SHA256: "s2", Class: ClassProject, Now: NowSame}},
			Outputs: []*Output{{Path: "/p/pk_dose.csv", SHA256: "pd", Class: ClassProject, Now: NowSame}}},
	}
	r := recipeOf(ts, "/p", "/p/pk_dose.csv", "")
	if len(r.Steps) != 2 || r.Verdict.Kind != "interactive" || len(r.Verdict.SessionVars) != 1 || r.Verdict.SessionVars[0].Name != "pk" {
		t.Fatalf("steps %d verdict %+v", len(r.Steps), r.Verdict)
	}
}

// The gap names who wrote the file before the hand edit, not who rewrote it afterwards.
func TestRecipe_HandEditGapNamesTheWriterBefore(t *testing.T) {
	ts := []*Task{
		{Session: "a", ID: "1", Start: 1, End: 2, Expr: `write.csv(x, "a.csv")`, Script: "/p/01.R",
			Outputs: []*Output{{Path: "/p/a.csv", SHA256: "v1", Class: ClassProject, Now: NowChanged}}},
		{Session: "b", ID: "1", Start: 10, End: 11, Expr: `y <- read.csv("a.csv")`, Script: "/p/02.R", Vars: []Var{{Name: "y"}},
			Inputs: []*Input{{Path: "/p/a.csv", SHA256: "v2", Class: ClassProject, Status: "changed-outside", Now: NowChanged}}},
		{Session: "b", ID: "2", Start: 11, End: 12, Expr: `write.csv(y, "a.csv"); write.csv(y, "out.csv")`, Script: "/p/02.R",
			Uses:    []VarUse{{Name: "y", Task: "1"}},
			Outputs: []*Output{{Path: "/p/a.csv", SHA256: "v3", Class: ClassProject}, {Path: "/p/out.csv", SHA256: "o", Class: ClassProject}}},
	}
	r := recipeOf(ts, "/p", "/p/out.csv", "")
	if len(r.Gaps) == 0 || !strings.Contains(r.Gaps[0], "01.R") {
		t.Fatalf("gaps %v", r.Gaps)
	}
}

// A command typed before a source() without a task boundary in between (getwd() in Positron)
// lands in the same task: the recipe runs both, in order, and the step is the script's.
func TestRecipe_AllCommandsOfATaskInOrder(t *testing.T) {
	ts := []*Task{
		{Session: "p", ID: "3", Start: 1, End: 5, Expr: "base::invisible(base::.ark_last_value)",
			Origins: []Origin{{Kind: "interactive", Code: "getwd()", TS: 2, Language: "r"},
				{Kind: "script", File: "/p/R/01_prepare.R", StartLine: 1, EndLine: 1, Code: `source("R/01_prepare.R", echo = TRUE)`, TS: 4, Language: "r"}},
			Sources: []Source{{Path: "/p/R/01_prepare.R", Class: ClassProject}},
			Inputs: []*Input{{Path: "/p/R/01_prepare.R", SHA256: "prep", Class: ClassProject, Now: NowSame},
				{Path: "/p/data/pk.csv", SHA256: "raw", Class: ClassProject, Now: NowSame}},
			Outputs: []*Output{{Path: "/p/derived/pk.csv", SHA256: "pk", Class: ClassProject, Now: NowSame}}},
	}
	r := recipeOf(ts, "/p", "/p/derived/pk.csv", "")
	want := "# --- Step 1 · " + time.Unix(0, 5).Format("15:04") + " · R/01_prepare.R\ngetwd()\nsource(\"R/01_prepare.R\", echo = TRUE)\n"
	if !strings.Contains(r.Script, want) {
		t.Fatalf("script lacks\n%s\nin\n%s", want, r.Script)
	}
	if o := ts[0].Origin(); o == nil || o.File != "/p/R/01_prepare.R" {
		t.Fatalf("the task's origin %+v", o)
	}
	if st := storyOf(ts, "/p", "/p/derived/pk.csv", ""); st == nil || st.Steps[0].Where != "R/01_prepare.R" {
		t.Fatalf("story step where: %+v", st)
	}
}

// A Python task with two commands: each its own python3, a %run as a file run, not as -c code
func TestRecipe_PythonCommandsOfATaskOneByOne(t *testing.T) {
	ts := []*Task{
		{Session: "py", ID: "1", Start: 1, End: 5,
			Origins: []Origin{{Kind: "interactive", Code: "import os", TS: 2, Language: "python"},
				{Kind: "script", File: "/p/py/06.py", Code: `%run -- "py/06.py"`, TS: 4, Language: "python"}},
			Inputs:  []*Input{{Path: "/p/py/06.py", SHA256: "py", Class: ClassProject, Now: NowSame}},
			Outputs: []*Output{{Path: "/p/derived/cov.csv", SHA256: "cov", Class: ClassProject, Now: NowSame}}},
	}
	r := recipeOf(ts, "/p", "/p/derived/cov.csv", "")
	for _, want := range []string{`shQuote(c("-c", "import os"))`, `shQuote(c("py/06.py"))`} {
		if !strings.Contains(r.Script, want) {
			t.Fatalf("script lacks %s:\n%s", want, r.Script)
		}
	}
	if strings.Contains(r.Script, `"-c", "import os\n%run`) {
		t.Fatalf("the two commands ran as one -c:\n%s", r.Script)
	}
}
