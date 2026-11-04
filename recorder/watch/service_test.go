package watch

import (
	"os"
	"path/filepath"
	"strings"
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

// ddmanymore 28.10.: 12,000 logs of a week's batch held 6.7 GB at startup and the service did not
// answer for minutes. An old log closes once read and keeps no reading state; with MaxAge it is
// not read at all, until it grows again.
func TestService_OldLogsCloseOnceReadAndMaxAgeLeavesThemOut(t *testing.T) {
	write := func(dir, id string, age time.Duration) string {
		l := filepath.Join(dir, id+".tsv")
		b := (&logb{}).ev(3999999, "open", "r", "/p/in.csv", "sha256=in").ev(3999999, "TASK", "ok", "1", "read") // a pid no process has
		os.WriteFile(l, []byte("#ktontrace v1\thash=/p\n"+strings.Join(b.lines, "\n")+"\n"), 0o644)
		at := time.Now().Add(-age)
		os.Chtimes(l, at, at)
		return l
	}
	newSv := func(dir string) *Service {
		return &Service{Dir: dir, Store: t.TempDir(), Idle: 15 * time.Minute, NewClassifier: func() *Classifier { return &Classifier{} }}
	}

	dir := t.TempDir()
	write(dir, "old", 48*time.Hour)
	write(dir, "new", 0)
	sv := newSv(dir)
	sv.Poll(time.Now())
	old, cur := sv.sessions["old"], sv.sessions["new"]
	if !old.s.Closed || old.f != nil || old.r != nil || old.s.cur != nil || old.s.known != nil {
		t.Fatalf("an old log is closed once read, without reading state: closed=%v", old.s.Closed)
	}
	if cur.s.Closed {
		t.Fatalf("a log written just now stays open")
	}

	dir = t.TempDir()
	l := write(dir, "old", 48*time.Hour)
	write(dir, "new", 0)
	sv = newSv(dir)
	sv.MaxAge = 24 * time.Hour
	sv.Poll(time.Now())
	if _, ok := sv.sessions["old"]; ok || sv.sessions["new"] == nil {
		t.Fatalf("MaxAge: sessions %v", len(sv.sessions))
	}
	f, _ := os.OpenFile(l, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("9\t100\t1\topen\tr\t/p/more.csv\t0\t0\t0\tsha256=m\n")
	f.Close()
	sv.Poll(time.Now())
	if sv.sessions["old"] == nil {
		t.Fatalf("a left-out log that grows is read")
	}
}
