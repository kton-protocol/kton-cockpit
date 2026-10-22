package watch

import (
	"os"
	"path/filepath"
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
