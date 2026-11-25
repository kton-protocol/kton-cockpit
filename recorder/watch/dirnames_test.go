package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A trace, store or repro directory whose path holds a pattern character is read as itself: with
// filepath.Glob over the joined path, "logs[1]" matched "logs1" and never itself (kton-review G1 on #254).
func TestService_DirectoryNamesAreNoPatterns(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "trace[1]")
	decoy := filepath.Join(root, "trace1") // what the pattern trace[1] would have matched
	for _, d := range []string{dir, decoy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	log := "#ktontrace v3\thash=/p\n10\t1\t1\topen\tw\t/p/a.csv\t0\t0\t0\tnew=1\n11\t1\t1\tclosew\tw\t/p/a.csv\t0\t0\t0\tsha256=a\n12\t1\t0\tTASK\tok\t1\t0\t0\t0\tclean\n13\t1\t0\tEND\t-\t-\t0\t0\t0\t-\n"
	os.WriteFile(filepath.Join(dir, "mine.tsv"), []byte(log), 0o644)
	os.WriteFile(filepath.Join(decoy, "other.tsv"), []byte(log), 0o644)
	store := filepath.Join(root, "store*")
	os.MkdirAll(store, 0o755)

	sv := &Service{Dir: dir, Store: store, NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	got := map[string]bool{}
	for _, s := range sv.Sessions() {
		got[s.ID] = true
	}
	if !got["mine"] || got["other"] || len(got) != 1 {
		t.Fatalf("sessions %v, want only mine (from %s)", got, dir)
	}

	// the repro folder beside it: repro is a sibling of trace[1]
	run := filepath.Join(root, "repro", "run1")
	os.MkdirAll(run, 0o755)
	os.WriteFile(filepath.Join(run, "kton-repro.json"), []byte(`{"project":"/p","target":"/p/a.csv","recorded":"a","reproduced":"a","same":true,"when":"2026-11-25T09:00:00Z"}`), 0o644)
	if rs := sv.Reproductions("/p", "/p/a.csv"); len(rs) != 1 {
		t.Errorf("reproductions %+v, want the one in %s", rs, run)
	}
}
