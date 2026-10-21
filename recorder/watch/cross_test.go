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
