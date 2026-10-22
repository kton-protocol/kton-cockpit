package watch

import (
	"os"
	"path/filepath"
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
