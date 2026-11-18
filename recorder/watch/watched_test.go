package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatched_WithoutAListEverythingIsRecorded_WithOneOnlyItsFolders(t *testing.T) {
	store := t.TempDir()
	hash := filepath.Join(t.TempDir(), "watched")
	w := OpenWatched(store, hash)
	if !w.Covers("/anywhere/x") || w.State().Set || !w.Wanted(&Session{ID: "x", Closed: true}) {
		t.Fatal("without a list everything is recorded, as before")
	}
	if err := w.SetFolder("/w/a/", true); err != nil {
		t.Fatal(err)
	}
	w.SetFolder("/w/b", true)
	w.SetFolder("/w/b", false)
	if !w.Covers("/w/a") || !w.Covers("/w/a/sub/f.R") || w.Covers("/w/ab") || w.Covers("/w/b/x") {
		t.Fatalf("covers: %+v", w.State())
	}
	if b, _ := os.ReadFile(hash); string(b) != "/w/a\n" {
		t.Fatalf("the shim's prefix file: %q", b)
	}
	w.SetKeep("s1", true)
	re := OpenWatched(store, "")
	if st := re.State(); !st.Set || len(st.Folders) != 1 || !re.Kept("s1") || re.Kept("s2") {
		t.Fatalf("read again: %+v", st)
	}
}

// A session that ran outside every watched folder is not stored, and once it has ended its log goes;
// one inside is stored as before. Keep and Delete act on what is stored.
func TestService_RecordsOnlyWatchedFoldersAndKeepsOrDeletesOnRequest(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	sv := &Service{Dir: dir, Store: store, Idle: time.Minute, Logf: t.Logf, NewClassifier: func() *Classifier {
		return &Classifier{System: DefaultSystem, Temp: DefaultTemp}
	}}
	if err := sv.Load(); err != nil {
		t.Fatal(err)
	}
	sv.Watched.SetFolder("/in", true)
	logOf := func(id, cwd string) string {
		p := filepath.Join(dir, id+".tsv")
		os.WriteFile(p, []byte("#ktontrace v3\thash=/in:/out\n"+
			"1\t4194305\t1\texec\t-\t"+cwd+"\t0\t0\t0\tRscript\\x20x.R\n"+
			"2\t4194305\t1\topen\tr\t"+cwd+"/a.csv\t0\t0\t0\tsha256=aaa\n"+
			"3\t4194305\t1\tEND\t-\t-\t0\t0\t0\t-\n"), 0o644)
		return p
	}
	inLog, outLog := logOf("in-1", "/in/p"), logOf("out-1", "/out/p")
	sv.Poll(time.Now())
	ss := sv.Sessions()
	if len(ss) != 1 || ss[0].ID != "in-1" {
		t.Fatalf("only the watched folder's session stays: %+v", ss)
	}
	if _, err := os.Stat(outLog); !os.IsNotExist(err) {
		t.Fatal("the log of a session outside every watched folder is deleted once it ended")
	}
	if _, err := os.Stat(filepath.Join(store, "out-1.json")); !os.IsNotExist(err) {
		t.Fatal("nothing is stored for it")
	}
	sv.Watched.SetKeep("in-1", true)
	if ss := sv.Sessions(); !ss[0].Kept {
		t.Fatalf("kept: %+v", ss)
	}
	if err := sv.DeleteSession("in-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inLog); !os.IsNotExist(err) || len(sv.Sessions()) != 0 || sv.Watched.Kept("in-1") {
		t.Fatal("Delete removes the session, its log and its keep")
	}
	if _, err := os.Stat(filepath.Join(store, "in-1.json")); !os.IsNotExist(err) {
		t.Fatal("Delete removes the stored observations")
	}
}
