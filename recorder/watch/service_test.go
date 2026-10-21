package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestService_FollowsLogsStoresAndReloads(t *testing.T) {
	dir, store := t.TempDir(), t.TempDir()
	newSv := func() *Service {
		return &Service{Dir: dir, Store: store, Idle: time.Minute, Logf: t.Logf, NewClassifier: func() *Classifier {
			return &Classifier{System: DefaultSystem, Temp: DefaultTemp}
		}}
	}
	sv := newSv()
	logf := filepath.Join(dir, "r-1.tsv")
	write := func(s string) {
		f, err := os.OpenFile(logf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	write("#ktontrace v1\thash=/p\n1\t999999\t1\topen\tr\t/p/a.csv\t0\t0\t0\tsha256=aaa\n2\t999999\t1\tTASK\tok\t1\t0\t0\t0\tread")
	now := time.Now()
	sv.Poll(now)
	if s := sv.Sessions(); len(s) != 1 || s[0].Tasks != 0 {
		t.Fatalf("an unfinished last line must wait: %+v", s)
	}
	write("\n3\t999999\t1\tEND\t-\t-\t0\t0\t0\t-\n")
	sv.Poll(now)
	s := sv.Sessions()
	if len(s) != 1 || s[0].Tasks != 1 || !s[0].Closed || s[0].Project != "/p" {
		t.Fatalf("after END: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(store, "r-1.json")); err != nil {
		t.Fatal(err)
	}
	os.Remove(logf) // only a session whose log is gone comes from the store
	re := newSv()
	if err := re.Load(); err != nil {
		t.Fatal(err)
	}
	re.Poll(now)
	if s := re.Sessions(); len(s) != 1 || s[0].Tasks != 1 {
		t.Fatalf("reloaded: %+v", s)
	}
}

func TestService_ClosesASilentSessionWhoseProcessesAreGone(t *testing.T) {
	dir := t.TempDir()
	sv := &Service{Dir: dir, Store: t.TempDir(), Idle: time.Second, NewClassifier: func() *Classifier { return &Classifier{} }}
	// pid 2^22+1 is above the kernel's pid_max default: never alive
	os.WriteFile(filepath.Join(dir, "x.tsv"), []byte("#ktontrace v1\thash=/p\n1\t4194305\t1\topen\tr\t/p/a\t0\t0\t0\tsha256=a\n"), 0o644)
	now := time.Now()
	sv.Poll(now)
	sv.Poll(now.Add(2 * time.Second))
	if s := sv.Sessions(); len(s) != 1 || !s[0].Closed || s[0].Tasks != 1 {
		t.Fatalf("%+v", s)
	}
}
