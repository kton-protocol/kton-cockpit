package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A trace, store or repro directory whose path holds a pattern character is read as itself: with
// filepath.Glob over the joined path, "w[1]" matched "w1" and never itself (kton-review G1 on #254).
// Each part has a decoy where the old pattern would have looked.
func TestService_DirectoryNamesAreNoPatterns(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := "#ktontrace v3\thash=/p\n10\t1\t1\topen\tw\t/p/a.csv\t0\t0\t0\tnew=1\n11\t1\t1\tclosew\tw\t/p/a.csv\t0\t0\t0\tsha256=a\n12\t1\t0\tTASK\tok\t1\t0\t0\t0\tclean\n13\t1\t0\tEND\t-\t-\t0\t0\t0\t-\n"
	dir := filepath.Join(root, "w[1]", "trace") // repro is its sibling: w[1]/repro
	write(filepath.Join(dir, "mine.tsv"), log)
	write(filepath.Join(root, "w1", "trace", "other.tsv"), log)

	// a closed session kept in the store, whose log is gone (Load takes those), and a decoy
	now := time.Now().UnixNano()
	kept := func(id string) string {
		return `{"id":"` + id + `","log":"/nonexistent/` + id + `.tsv","project":"/p","start_ns":` + strconv.FormatInt(now, 10) + `,"last_ns":` + strconv.FormatInt(now, 10) + `,"closed":true}`
	}
	store := filepath.Join(root, "store*")
	write(filepath.Join(store, "kept.json"), kept("kept"))
	write(filepath.Join(root, "storeX", "decoy.json"), kept("decoy"))

	repro := func(name string) string {
		return `{"project":"/p","target":"/p/a.csv","recorded":"a","reproduced":"a","same":true,"when":"2026-11-25T09:00:00Z","dir":"` + name + `"}`
	}
	write(filepath.Join(root, "w[1]", "repro", "run1", "kton-repro.json"), repro("run1"))
	write(filepath.Join(root, "w1", "repro", "decoy", "kton-repro.json"), repro("decoy"))

	sv := &Service{Dir: dir, Store: store, NewClassifier: func() *Classifier { return &Classifier{} }}
	if err := sv.Load(); err != nil {
		t.Fatal(err)
	}
	sv.Poll(time.Now())
	got := map[string]bool{}
	for _, s := range sv.Sessions() {
		got[s.ID] = true
	}
	if !got["mine"] || !got["kept"] || got["other"] || got["decoy"] || len(got) != 2 {
		t.Errorf("sessions %v, want mine (Poll of %s) and kept (Load of %s) only", got, dir, store)
	}
	rs := sv.Reproductions("/p", "/p/a.csv")
	if len(rs) != 1 || rs[0].Dir != "run1" {
		t.Errorf("reproductions %+v, want the one under w[1]/repro only", rs)
	}
}
