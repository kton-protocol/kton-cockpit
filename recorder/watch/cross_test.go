package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Morning session writes a dataset, afternoon session reads it: the afternoon's input comes
// from the morning's task, and the lineage crosses the sessions.
func TestService_ProvenanceAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name), []byte("#ktontrace v1\thash=/p\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write("morgen.tsv",
		"10\t1\t1\topen\tr\t/p/raw.csv\t0\t0\t0\tsha256=raw",
		"11\t1\t1\topen\tw\t/p/clean.csv\t0\t0\t0\tnew=1",
		"12\t1\t1\tclosew\tw\t/p/clean.csv\t0\t0\t0\tsha256=clean",
		"13\t1\t1\tTASK\tok\t1\t0\t0\t0\tclean",
		"14\t1\t1\tEND\t-\t-\t0\t0\t0\t-")
	write("mittag.tsv",
		"20\t2\t1\topen\tr\t/p/clean.csv\t0\t0\t0\tsha256=clean",
		"21\t2\t1\topen\tw\t/p/fit.rds\t0\t0\t0\tnew=1",
		"22\t2\t1\tclosew\tw\t/p/fit.rds\t0\t0\t0\tsha256=fit",
		"23\t2\t1\tTASK\tok\t1\t0\t0\t0\tfit",
		"24\t2\t1\tEND\t-\t-\t0\t0\t0\t-")
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	var in *Input
	sv.With("mittag", func(s *Session) { in = sv.annotated(NewChecker(), s, s.Tasks)[0].Inputs[0] })
	if len(in.From) != 1 || in.From[0].Session != "morgen" || in.From[0].Task != "1" {
		t.Fatalf("afternoon input: %+v", in)
	}
	var st *Step
	sv.With("mittag", func(s *Session) { st = sv.lineageAcross("mittag", "/p/fit.rds") })
	if st == nil || len(st.From) != 1 || st.From[0].Session != "morgen" || st.From[0].File != "/p/clean.csv" {
		t.Fatalf("lineage %+v", st)
	}
}

func TestService_HandEditBetweenSessions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.tsv"), []byte("#ktontrace v1\thash=/p\n10\t1\t1\topen\tw\t/p/x.csv\t0\t0\t0\tnew=1\n11\t1\t1\tclosew\tw\t/p/x.csv\t0\t0\t0\tsha256=v1\n12\t1\t1\tTASK\tok\t1\t0\t0\t0\tw\n13\t1\t1\tEND\t-\t-\t0\t0\t0\t-\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.tsv"), []byte("#ktontrace v1\thash=/p\n20\t2\t1\topen\tr\t/p/x.csv\t0\t0\t0\tsha256=v2\n21\t2\t1\tTASK\tok\t1\t0\t0\t0\tr\n22\t2\t1\tEND\t-\t-\t0\t0\t0\t-\n"), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	var in *Input
	sv.With("b", func(s *Session) { in = sv.annotated(NewChecker(), s, s.Tasks)[0].Inputs[0] })
	if in.Status != "changed-outside" || len(in.From) != 0 {
		t.Fatalf("%+v", in)
	}
}

// From MH's Positron run: R's second command reads what a Python console wrote after R's
// first command ended but before the second was sent; the recipe still crosses into Python.
// Then the first step is run again (same bytes) and the Python step too (other bytes): the
// history keeps the versions it used and says that they were written again.
func TestService_HistoryCrossesConsolesAndMarksLaterWrites(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name), []byte("#ktontrace v3\thash=/p\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write("r.tsv",
		"10\t1\t0\tENV\t-\t/usr/lib/R\t0\t0\t0\tproject=/p",
		"11\t1\t1\topen\tw\t/p/clean.csv\t0\t0\t0\tnew=1",
		"12\t1\t1\tclosew\tw\t/p/clean.csv\t0\t0\t0\tsha256=clean",
		"13\t1\t0\tTASK\tok\t1\t0\t0\t0\tclean",
		"31\t1\t1\topen\tr\t/p/nca.csv\t0\t0\t0\tsha256=nca",
		"32\t1\t1\topen\tw\t/p/sum.csv\t0\t0\t0\tnew=1",
		"33\t1\t1\tclosew\tw\t/p/sum.csv\t0\t0\t0\tsha256=sum",
		"34\t1\t0\tTASK\tok\t2\t0\t0\t0\tsummary",
		"35\t1\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("py.tsv",
		"15\t2\t0\tENV\t-\t/usr\t0\t0\t0\tproject=/p;lang=python",
		"20\t2\t1\topen\tr\t/p/clean.csv\t0\t0\t0\tsha256=clean",
		"21\t2\t1\topen\tw\t/p/nca.csv\t0\t0\t0\tnew=1",
		"22\t2\t1\tclosew\tw\t/p/nca.csv\t0\t0\t0\tsha256=nca",
		"23\t2\t0\tTASK\tok\t1\t0\t0\t0\tnca",
		"24\t2\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("r2.tsv", // step 1 again: the same bytes
		"40\t3\t0\tENV\t-\t/usr/lib/R\t0\t0\t0\tproject=/p",
		"41\t3\t1\topen\tw\t/p/clean.csv\t0\t0\t0\ttrunc=1",
		"42\t3\t1\tclosew\tw\t/p/clean.csv\t0\t0\t0\tsha256=clean",
		"43\t3\t0\tTASK\tok\t1\t0\t0\t0\tclean",
		"44\t3\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("py2.tsv", // the Python step again: other bytes
		"50\t4\t0\tENV\t-\t/usr\t0\t0\t0\tproject=/p;lang=python",
		"51\t4\t1\topen\tw\t/p/nca.csv\t0\t0\t0\ttrunc=1",
		"52\t4\t1\tclosew\tw\t/p/nca.csv\t0\t0\t0\tsha256=nca2",
		"53\t4\t0\tTASK\tok\t1\t0\t0\t0\tnca",
		"54\t4\t0\tEND\t-\t-\t0\t0\t0\t-")
	os.WriteFile(filepath.Join(dir, OriginsFile), []byte(`{"ts_ns":11,"kind":"script","language":"r","code":"source(\"R/01.R\")","file":"/p/R/01.R"}
{"ts_ns":30,"kind":"script","language":"r","code":"source(\"R/03.R\")","file":"/p/R/03.R"}
`), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	r := sv.Recipe("/p", "/p/sum.csv", "")
	if r == nil {
		t.Fatal("no recipe")
	}
	var steps []string
	for _, s := range r.Steps {
		steps = append(steps, s.Session+"/"+s.ID)
	}
	if strings.Join(steps, ",") != "r/1,py/1,r/2" {
		t.Fatalf("steps %v: R's second command was sent after the Python step wrote nca.csv", steps)
	}
	for _, s := range r.Steps {
		for _, in := range s.Inputs {
			switch {
			case s.Session == "py" && in.Path == "/p/clean.csv":
				if in.Later == nil || !in.Later.Same || in.Later.Session != "r2" {
					t.Errorf("clean.csv as Python read it: later %+v, want rewritten by r2 with the same bytes", in.Later)
				}
			case s.Session == "r" && in.Path == "/p/nca.csv":
				if in.Later == nil || in.Later.Same || in.Later.Session != "py2" {
					t.Errorf("nca.csv as R read it: later %+v, want overwritten by py2", in.Later)
				}
			}
		}
	}
}

// From MH: a step read pk_clean.csv, the first step ran again (same bytes), then the file was
// edited by hand. Nothing recorded explains today's bytes: the history and the story say the
// file was changed afterwards outside the recording.
func TestService_HistoryNamesAChangeOutsideTheRecording(t *testing.T) {
	dir, proj := t.TempDir(), t.TempDir()
	clean := filepath.Join(proj, "clean.csv")
	os.WriteFile(clean, []byte("edited by hand\n"), 0o644) // today's bytes: none of the recorded ones
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name), []byte("#ktontrace v3\thash="+proj+"\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write("r.tsv",
		"10\t1\t0\tENV\t-\t/usr/lib/R\t0\t0\t0\tproject="+proj,
		"11\t1\t1\topen\tw\t"+clean+"\t0\t0\t0\tnew=1",
		"12\t1\t1\tclosew\tw\t"+clean+"\t0\t0\t0\tsha256=clean",
		"13\t1\t0\tTASK\tok\t1\t0\t0\t0\tclean",
		"20\t1\t1\topen\tr\t"+clean+"\t0\t0\t0\tsha256=clean",
		"21\t1\t1\topen\tw\t"+proj+"/plot.png\t0\t0\t0\tnew=1",
		"22\t1\t1\tclosew\tw\t"+proj+"/plot.png\t0\t0\t0\tsha256=png",
		"23\t1\t0\tTASK\tok\t2\t0\t0\t0\tplot",
		"24\t1\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("r2.tsv", // step 1 again, the same bytes; the hand edit came after
		"40\t3\t0\tENV\t-\t/usr/lib/R\t0\t0\t0\tproject="+proj,
		"41\t3\t1\topen\tw\t"+clean+"\t0\t0\t0\ttrunc=1",
		"42\t3\t1\tclosew\tw\t"+clean+"\t0\t0\t0\tsha256=clean",
		"43\t3\t0\tTASK\tok\t1\t0\t0\t0\tclean",
		"44\t3\t0\tEND\t-\t-\t0\t0\t0\t-")
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	r := sv.Recipe(proj, proj+"/plot.png", "")
	if r == nil || len(r.Steps) != 2 {
		t.Fatalf("recipe %+v", r)
	}
	in := r.Steps[1].Inputs[0]
	if in.Later == nil || !in.Later.Unrecorded {
		t.Fatalf("clean.csv as the plot read it: later %+v, want changed outside the recording", in.Later)
	}
	st := sv.Story(proj, proj+"/plot.png", "")
	var warned bool
	for _, s := range st.Steps {
		for _, w := range s.Warnings {
			warned = warned || strings.Contains(w, "outside the recording")
		}
	}
	if !warned {
		t.Fatalf("the story does not say it: %+v", st.Steps)
	}
}

// Review L1 of #18: a long step in another session wrote x before a read and ended after it.
// That write is not later than the read: by write time, not by the step's end.
func TestService_LaterGoesByWriteTimeNotByEnd(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name), []byte("#ktontrace v3\thash=/p\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write("long.tsv",
		"20\t1\t1\topen\tw\t/p/x.csv\t0\t0\t0\tnew=1",
		"21\t1\t1\tclosew\tw\t/p/x.csv\t0\t0\t0\tsha256=x2",
		"100\t1\t0\tTASK\tok\t1\t0\t0\t0\tlong",
		"101\t1\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("reader.tsv",
		"10\t2\t1\topen\tw\t/p/x.csv\t0\t0\t0\tnew=1",
		"11\t2\t1\tclosew\tw\t/p/x.csv\t0\t0\t0\tsha256=x1",
		"12\t2\t0\tTASK\tok\t1\t0\t0\t0\tfirst",
		"50\t2\t1\topen\tr\t/p/x.csv\t0\t0\t0\tsha256=x2",
		"51\t2\t1\topen\tw\t/p/y.csv\t0\t0\t0\tnew=1",
		"52\t2\t1\tclosew\tw\t/p/y.csv\t0\t0\t0\tsha256=y",
		"53\t2\t0\tTASK\tok\t2\t0\t0\t0\tread",
		"54\t2\t0\tEND\t-\t-\t0\t0\t0\t-")
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	for _, tk := range sv.projectTasks(NewChecker(), "/p") {
		for _, in := range tk.Inputs {
			if in.Path == "/p/x.csv" && in.Later != nil && !in.Later.Unrecorded {
				t.Fatalf("x.csv as %s/%s read it: later %+v, but that write came before the read", tk.Session, tk.ID, in.Later)
			}
		}
	}
}

// Review U1 of #18: T1 writes v1, T2 reruns it (v1 again), T3 writes v2, and the disk holds v2.
// The change was recorded (T3): neither T1's output nor a reader of v1 between T1 and T2 may
// claim a change outside the recording.
func TestService_ARecordedChangeAfterARerunIsNotAHandEdit(t *testing.T) {
	dir, proj := t.TempDir(), t.TempDir()
	out := filepath.Join(proj, "out.csv")
	os.WriteFile(out, []byte("v2\n"), 0o644)
	v1, v2 := hexOf("v1\n"), hexOf("v2\n")
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name), []byte("#ktontrace v3\thash="+proj+"\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	step := func(ts int, pid int, sum, id string) []string {
		p := strconv.Itoa(pid)
		return []string{
			strconv.Itoa(ts) + "\t" + p + "\t1\topen\tw\t" + out + "\t0\t0\t0\tnew=1",
			strconv.Itoa(ts+1) + "\t" + p + "\t1\tclosew\tw\t" + out + "\t0\t0\t0\tsha256=" + sum,
			strconv.Itoa(ts+2) + "\t" + p + "\t0\tTASK\tok\t" + id + "\t0\t0\t0\twrite",
		}
	}
	write("t1.tsv", append(step(10, 1, v1, "1"), "13\t1\t0\tEND\t-\t-\t0\t0\t0\t-")...)
	write("reader.tsv",
		"20\t5\t1\topen\tr\t"+out+"\t0\t0\t0\tsha256="+v1,
		"21\t5\t0\tTASK\tok\t1\t0\t0\t0\tread",
		"22\t5\t0\tEND\t-\t-\t0\t0\t0\t-")
	write("t2.tsv", append(step(30, 2, v1, "1"), "33\t2\t0\tEND\t-\t-\t0\t0\t0\t-")...)
	write("t3.tsv", append(step(40, 3, v2, "1"), "43\t3\t0\tEND\t-\t-\t0\t0\t0\t-")...)
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	for _, tk := range sv.projectTasks(NewChecker(), proj) {
		var l *Later
		switch tk.Session {
		case "t1":
			l = tk.Outputs[0].Later
		case "reader":
			l = tk.Inputs[0].Later
		default:
			continue
		}
		if l == nil || l.Unrecorded || l.Same || l.Session != "t3" {
			t.Errorf("%s: later %+v, want overwritten by t3 (recorded)", tk.Session, l)
		}
	}
}

// MH: a reproduction belongs in the story: when, and which step gave the same bytes. The
// extension's kton-repro.json lies beside the logs (…/repro/<run>/).
func TestService_TheStoryCarriesReproductions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "r.tsv"), []byte("#ktontrace v3\thash=/p\n"+strings.Join([]string{
		"10\t1\t1\topen\tw\t/p/a.csv\t0\t0\t0\tnew=1",
		"11\t1\t1\tclosew\tw\t/p/a.csv\t0\t0\t0\tsha256=a",
		"12\t1\t0\tTASK\tok\t1\t0\t0\t0\tclean",
		"20\t1\t1\topen\tr\t/p/a.csv\t0\t0\t0\tsha256=a",
		"21\t1\t1\topen\tw\t/p/b.csv\t0\t0\t0\tnew=1",
		"22\t1\t1\tclosew\tw\t/p/b.csv\t0\t0\t0\tsha256=b",
		"23\t1\t0\tTASK\tok\t2\t0\t0\t0\tstamp",
		"24\t1\t0\tEND\t-\t-\t0\t0\t0\t-"}, "\n")+"\n"), 0o644)
	run := filepath.Join(filepath.Dir(dir), "repro", "2026-10-23-b.csv")
	os.MkdirAll(run, 0o755)
	os.WriteFile(filepath.Join(run, "kton-repro.json"), []byte(`{"project":"/p","target":"/p/b.csv","recorded":"b","reproduced":"b2","same":false,
 "when":"2026-10-23T09:00:00Z","dir":"`+run+`","seconds":3,
 "steps":[{"session":"r","task":"1","outputs":[{"path":"/p/a.csv","recorded":"a","reproduced":"a","same":true}]},
          {"session":"r","task":"2","outputs":[{"path":"/p/b.csv","recorded":"b","reproduced":"b2","same":false}]}]}`), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	st := sv.Story("/p", "/p/b.csv", "")
	if st == nil || len(st.Reproductions) != 1 || st.Reproductions[0].Same {
		t.Fatalf("reproductions %+v", st)
	}
	by := map[string]*StepRepro{}
	for _, s := range st.Steps {
		by[s.Task] = s.Repro
	}
	if r := by["1"]; r == nil || !r.Same {
		t.Errorf("step 1: %+v, want reproduced with the same bytes", r)
	}
	if r := by["2"]; r == nil || r.Same || len(r.Different) != 1 || r.Different[0] != "b.csv" {
		t.Errorf("step 2: %+v, want b.csv different", r)
	}
}
