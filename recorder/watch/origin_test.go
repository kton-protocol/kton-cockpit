package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const qmd = "---\ntitle: x\n---\n\n```{r}\npkd2 <- read.csv(\"data/pk_dose.csv\")\nsummary(pkd2$conc)\n```\n\n```{r}\n#| label: herkunft\nf <- function() NULL\n```\n\n```{r setup, include=FALSE}\nlibrary(x)\n```\n"

func TestChunks_LabelsAndLines(t *testing.T) {
	cs := Chunks(qmd)
	if len(cs) != 3 {
		t.Fatalf("%+v", cs)
	}
	if cs[0].Label != "#1" || cs[0].StartLine != 6 || cs[0].EndLine != 7 {
		t.Fatalf("chunk 1 %+v", cs[0])
	}
	if cs[1].Label != "herkunft" || cs[2].Label != "setup" {
		t.Fatalf("labels %q %q", cs[1].Label, cs[2].Label)
	}
}

// The S6 probe's raw lines, as Positron 2026.10 produced them.
func TestOrigins_FromProbeLinesAndChunkMatch(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "step3.qmd")
	if err := os.WriteFile(doc, []byte(qmd), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := `{"ts":1000,"event":"activated"}
{"ts":2000,"event":"executed","payload":{"attribution":{"source":"interactive"},"code":"getwd()","languageId":"r"}}
{"ts":3000,"event":"executed","payload":{"attribution":{"source":"script","metadata":{"codeLocation":{"uri":{"path":"` + doc + `","scheme":"file"},"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}},"code":"pkd2 <- read.csv(\"data/pk_dose.csv\")\nsummary(pkd2$conc)","languageId":"r"}}
{"ts":4000,"event":"executed","payload":{"attribution":{"source":"script","metadata":{"codeLocation":{"uri":{"path":"/p/herkunft.R","scheme":"file"},"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":96}}}}},"code":"c(1)","languageId":"r"}}
`
	jl := filepath.Join(dir, "exec.jsonl")
	if err := os.WriteFile(jl, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	ors, err := ParseOrigins(jl)
	if err != nil || len(ors) != 3 {
		t.Fatalf("%v %+v", err, ors)
	}
	tasks := []*Task{
		{ID: "1", Start: 0, End: 2_500_000_000},
		{ID: "2", Start: 2_500_000_000, End: 3_500_000_000},
		{ID: "3", Start: 3_500_000_000, End: 3_600_000_000}, // Ark's second marker: nothing submitted
		{ID: "4", Start: 3_600_000_000, End: 5_000_000_000},
	}
	AttachOrigins(tasks, ors)
	if len(tasks[0].Origins) != 1 || tasks[0].Origins[0].Kind != "interactive" {
		t.Fatalf("task 1 %+v", tasks[0].Origins)
	}
	if o := tasks[1].Origins; len(o) != 1 || o[0].File != doc || o[0].Chunk != "#1" || o[0].StartLine != 6 {
		t.Fatalf("task 2 should be chunk #1 of step3.qmd: %+v", o)
	}
	if len(tasks[2].Origins) != 0 {
		t.Fatalf("task 3 %+v", tasks[2].Origins)
	}
	if o := tasks[3].Origins; len(o) != 1 || o[0].StartLine != 2 || o[0].EndLine != 2 {
		t.Fatalf("task 4 lines %+v", o)
	}
}

// The format recorder/positron/extension.js writes.
func TestOrigins_RecorderFormat(t *testing.T) {
	jl := filepath.Join(t.TempDir(), "exec.jsonl")
	os.WriteFile(jl, []byte(`{"ts_ns":5,"kind":"script","language":"r","file":"/p/a.R","start_line":3,"end_line":4,"code":"x <- 1"}
{"ts_ns":6,"kind":"interactive","language":"python","code":"print(1)"}
`), 0o644)
	o, err := ParseOrigins(jl)
	if err != nil || len(o) != 2 || o[0].File != "/p/a.R" || o[0].StartLine != 3 || o[0].EndLine != 4 ||
		o[0].Language != "r" || o[1].Language != "python" {
		t.Fatalf("%v %+v", err, o)
	}
}

// A Python console session (ktonrec.py's ENV says lang=python) gets the Python executions the
// extension logged, not the R console's, and its draft runs the script with python3.
func TestOrigins_APythonSessionGetsPythonExecutions(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "positron-exec.jsonl")
	os.WriteFile(f, []byte(`{"ts_ns":15,"kind":"script","language":"python","code":"%run -- \"py/02_nca.py\"","file":"/p/py/02_nca.py"}
{"ts_ns":16,"kind":"script","language":"r","code":"source(\"R/03.R\")","file":"/p/R/03.R"}
{"ts_ns":17,"kind":"x","language":"julia","code":"1"}
`), 0o644)
	all, err := ParseOrigins(f)
	if err != nil || len(all) != 2 {
		t.Fatalf("origins %+v %v: julia is skipped, R and Python kept", all, err)
	}
	s := NewSession("t", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp})
	s.Line("#ktontrace v3\thash=/p")
	s.Line("10\t200\t0\tENV\t-\t/usr\t0\t0\t0\tproject=/p;lang=python")
	s.Line("20\t200\t0\tTASK\tok\t1\t0\t0\t0\t%run -- \"py/02_nca.py\"")
	AttachOrigins(s.Tasks, OriginsIn(all, s.Language))
	t1 := s.Tasks[0]
	if s.Language != "python" || len(t1.Origins) != 1 || t1.Origins[0].Language != "python" {
		t.Fatalf("lang %q origins %+v", s.Language, t1.Origins)
	}
	if got := draftCmd(t1, "/p"); got != "python3 py/02_nca.py" {
		t.Fatalf("cmd %q", got)
	}
}

// Review P1 of #15: python3 only for a %run of a .py file, with its arguments; anything else is
// where the code was or the code itself, never R's source(), and the draft says to check it.
func TestDraftCmd_PythonRunsOnlyWhatPercentRunRan(t *testing.T) {
	task := func(o Origin) *Task { o.Language = "python"; return &Task{ID: "1", Origins: []Origin{o}} }
	for _, row := range []struct {
		o    Origin
		want string
	}{
		{Origin{Code: `%run -- "py/fit.py" --n 5`, File: "/p/py/fit.py"}, "python3 py/fit.py --n 5"},
		{Origin{Code: `%run -i -- "py/fit.py"`, File: "/p/py/fit.py"}, "python3 py/fit.py"},
		{Origin{Code: "x = 1", File: "/p/report.qmd"}, "x = 1"},
		{Origin{Code: "%run -m pkg.tool", File: "/p/pkg/tool.py"}, "%run -m pkg.tool"},
		{Origin{Code: "%run -t -N 5 fit.py", File: "/p/py/fit.py"}, "%run -t -N 5 fit.py"},
		{Origin{Code: "%run py/ok.py\nprint(1)", File: "/p/py/ok.py"}, "%run py/ok.py\nprint(1)"}, // review of #18: two lines are console code
		{Origin{Code: "x = 1", File: "/p/py/a.py", StartLine: 3, EndLine: 4}, "py/a.py:3-4"},
		{Origin{Code: "x = 1", File: "/p/report.qmd", Chunk: "fit"}, "chunk fit of report.qmd"},
	} {
		if got := draftCmd(task(row.o), "/p"); got != row.want {
			t.Errorf("%q: got %q, want %q", row.o.Code, got, row.want)
		}
	}
	s := NewSession("t", "/log.tsv", &Classifier{})
	for code, want := range map[string]string{
		`%run -i -- "py/fit.py"`: "-i",                // runs in the console's namespace, not as python3
		"x = 1":                  "not a program run", // console code
		"%run -m pkg.tool":       "-m",                // a %run, not console code (review of #15)
		"%run fit.py $n *.csv":   "patterns",          // %run expands before the script sees them
		`%run -- "py/fit.py" 5`:  "",                  // a plain run: nothing to check
	} {
		notes := strings.Join(MakeDraft(s, task(Origin{Code: code, File: "/p/py/fit.py"})).Notes, ";")
		if (want == "") != (notes == "") || !strings.Contains(notes, want) {
			t.Errorf("%q: notes %q, want %q", code, notes, want)
		}
	}
}

// From MH's Positron run: Ark reads the workspace's R files while the console waits (indexing
// at start, a re-read after a file ran). With the execution's time from the extension, those
// reads are not the command's; a file the command read itself stays.
func TestAttachOrigins_ReadsWhileTheConsoleWaitedAreNotTheCommands(t *testing.T) {
	const s = 1_000_000_000
	l := &logb{}
	at := func(ts int64, op, mode, path, extra string) {
		l.ts = ts - 1
		l.ev(100, op, mode, path, extra)
	}
	at(1*s, "ENV", "-", "/usr/lib/R", "project=/p")
	at(2*s, "open", "r", "/p/R/05_plot.R", "sha256=plot") // indexing
	at(2*s+1, "open", "r", "/p/R/01_clean.R", "sha256=clean")
	at(5*s+2_000_000, "open", "r", "/p/R/01_clean.R", "sha256=clean") // the command
	at(5*s+3_000_000, "open", "r", "/p/data/pk.csv", "sha256=pk")
	at(5*s+4_000_000, "TASK", "ok", "1", "base::invisible(base::.ark_last_value)")
	at(7*s, "open", "r", "/p/R/01_clean.R", "sha256=clean") // the re-read after it ran
	at(9*s+2_000_000, "open", "r", "/p/R/03_summary.R", "sha256=sum")
	at(9*s+3_000_000, "TASK", "ok", "2", "base::invisible(base::.ark_last_value)")
	se := sess(t, l, nil)
	AttachOrigins(se.Tasks, []Origin{
		{TS: 5 * s, File: "/p/R/01_clean.R", Code: `source("R/01_clean.R")`, Language: "r"},
		{TS: 9 * s, File: "/p/R/03_summary.R", Code: `source("R/03_summary.R")`, Language: "r"},
	})
	names := func(tk *Task) (out []string) {
		for _, in := range tk.Inputs {
			out = append(out, filepath.Base(in.Path))
		}
		return out
	}
	if got := strings.Join(names(task(t, se, "1")), ","); got != "01_clean.R,pk.csv" {
		t.Fatalf("task 1 inputs %s", got)
	}
	if got := strings.Join(names(task(t, se, "2")), ","); got != "03_summary.R" {
		t.Fatalf("task 2 inputs %s", got)
	}
}

// Review I1 of #16: the extension notes the send time late, so a command's own first read can
// lie before it. Only what Ark reads while waiting is dropped, a data file never.
func TestAttachOrigins_ALateOriginNeverDropsData(t *testing.T) {
	const s = 1_000_000_000
	l := &logb{}
	at := func(ts int64, op, mode, path, extra string) {
		l.ts = ts - 1
		l.ev(100, op, mode, path, extra)
	}
	at(1*s, "ENV", "-", "/usr/lib/R", "project=/p")
	at(4*s, "open", "r", "/p/R/other.R", "sha256=o")                // Ark, while waiting
	at(5*s-300_000_000, "open", "r", "/p/data/pk.csv", "sha256=pk") // the command, before the late origin
	at(5*s+4_000_000, "TASK", "ok", "1", "base::invisible(base::.ark_last_value)")
	se := sess(t, l, nil)
	AttachOrigins(se.Tasks, []Origin{{TS: 5 * s, File: "/p/R/01.R", Code: `source("R/01.R")`, Language: "r"}})
	var names []string
	for _, in := range task(t, se, "1").Inputs {
		names = append(names, filepath.Base(in.Path))
	}
	if strings.Join(names, ",") != "pk.csv" {
		t.Fatalf("inputs %v: the data file stays, Ark's read goes", names)
	}
}

// From MH's reproduction of summary.md: the recipe is an R script, and a Python console step in
// it must run Python, not land there as `%run …`.
func TestPythonStep_RunsPythonFromTheRecipe(t *testing.T) {
	if got := pythonStep(`%run -- "py/02_nca.py" --n 5`); got != `stopifnot(system2("python3", shQuote(c("py/02_nca.py", "--n", "5"))) == 0)  # %run -- "py/02_nca.py" --n 5`+"\n" {
		t.Errorf("%%run: %q", got)
	}
	if got := pythonStep("%run py/ok.py\nprint(\"R ran this\")"); !strings.Contains(got, `shQuote(c("-c", `) || strings.Contains(got, "\nprint") {
		t.Errorf("a multi-line %%run cell must run as Python code, its second line not as R: %q", got)
	}
	got := pythonStep("import csv\nprint('x')")
	if !strings.Contains(got, `c("-c", "import csv\nprint('x')")`) || !strings.Contains(got, "without the console's variables") {
		t.Errorf("console code: %q", got)
	}
}

// MH's session (positron-59277): R logged the source of 03 inside step 3, but its line and the
// Positron origin were stamped 0.1 s after step 3's end (the clocks disagree by tenths of a second
// in WSL). By time alone the origin went to step 4, which then had two commands, and step 3 none.
func TestAttachOrigins_AScriptGoesToTheStepThatSourcedIt(t *testing.T) {
	const s = int64(1_000_000_000)
	f03, f04 := "/p/R/03_fit_individual.R", "/p/R/04_fit_population.R"
	t3 := &Task{ID: "3", Start: 263_020 * s / 1000, End: 268_110 * s / 1000, Sources: []Source{{Path: f03, Class: ClassProject}},
		Inputs: []*Input{{Path: f03, SHA256: "a", LastRead: 268_210 * s / 1000}, {Path: "/p/R/02_nca.R", SHA256: "b", LastRead: 265_890 * s / 1000}}}
	t4 := &Task{ID: "4", Start: 268_110 * s / 1000, End: 273_090 * s / 1000, Sources: []Source{{Path: f04, Class: ClassProject}}}
	AttachOrigins([]*Task{t3, t4}, []Origin{
		{Kind: "script", File: f03, Code: `source("R/03_fit_individual.R", echo = TRUE)`, TS: 268_210 * s / 1000},
		{Kind: "script", File: f04, Code: `source("R/04_fit_population.R", echo = TRUE)`, TS: 273_010 * s / 1000},
	})
	if len(t3.Origins) != 1 || t3.Origins[0].File != f03 {
		t.Fatalf("step 3 origins %+v", t3.Origins)
	}
	if len(t4.Origins) != 1 || t4.Origins[0].File != f04 {
		t.Fatalf("step 4 origins %+v", t4.Origins)
	}
	// Ark's indexing read of 02_nca.R while the console waited is still not step 3's input
	for _, in := range t3.Inputs {
		if strings.HasSuffix(in.Path, "02_nca.R") {
			t.Fatalf("Ark's read kept: %+v", t3.Inputs)
		}
	}
}
