package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

func TestPrune_ByAgeKeepCASAndLog(t *testing.T) {
	root := t.TempDir()
	o := pruneOpts{Dir: filepath.Join(root, "trace"), Store: filepath.Join(root, "obs"), CAS: filepath.Join(root, "cas"),
		Log: filepath.Join(root, "ktonwatch.log"), Archive: filepath.Join(root, "archive"),
		Older: 72 * time.Hour, Keep: []string{"/w/keep"}, CASTotal: 250, LogMax: 400}
	for _, d := range []string{o.Dir, o.Store, o.CAS, filepath.Join(o.Archive, "old")} {
		os.MkdirAll(d, 0o755)
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	put := func(p, body string, at time.Time) { os.WriteFile(p, []byte(body), 0o644); os.Chtimes(p, at, at) }
	put(filepath.Join(o.Dir, "a.tsv"), "#h\n1\t1\t1\texec\tq\t/w/other\t0\t0\t0\tRscript x.R\n", old)
	put(filepath.Join(o.Store, "a.json"), `{"project":"/w/other"}`, old)
	put(filepath.Join(o.Dir, "b.tsv"), "#h\n", old)
	put(filepath.Join(o.Store, "b.json"), `{"project":"/w/keep/sub"}`, old)
	put(filepath.Join(o.Dir, "c.tsv"), "#h\n", time.Now())
	put(filepath.Join(o.Store, "notes.json"), "{}", old)
	for i, at := range []time.Time{old, old.Add(time.Hour), time.Now()} {
		put(filepath.Join(o.CAS, string(rune('x'+i))), strings.Repeat("z", 100), at)
	}
	put(o.Log, strings.Repeat("l", 1000), time.Now())
	put(filepath.Join(o.Archive, "old", "f"), "a", old)

	r, err := prune(o)
	if err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(filepath.Join(o.Dir, "a.tsv")) || exists(filepath.Join(o.Store, "a.json")) {
		t.Fatalf("the old session of another project stays: %v", r.Lines)
	}
	if !exists(filepath.Join(o.Dir, "b.tsv")) || !exists(filepath.Join(o.Dir, "c.tsv")) || !exists(filepath.Join(o.Store, "notes.json")) {
		t.Fatalf("a kept project's session, a new log or the notes went: %v", r.Lines)
	}
	if exists(filepath.Join(o.CAS, "x")) || !exists(filepath.Join(o.CAS, "z")) {
		t.Fatalf("cas: the oldest goes first, down to the cap: %v", r.Lines)
	}
	if fi, _ := os.Stat(o.Log); fi.Size() != 0 || !exists(o.Log+".1") {
		t.Fatalf("log not rotated")
	}
	if !exists(filepath.Join(o.Archive, "old")) {
		t.Fatalf("archive/ deleted without being named")
	}
	o.Archives = []string{"old"}
	if _, err := prune(o); err != nil || exists(filepath.Join(o.Archive, "old")) {
		t.Fatalf("named archive not deleted: %v", err)
	}
}

// Sessions the person kept, and those with draft notes, stay whatever their age (MH 18.11.).
func TestPrune_KeepsKeptSessionsAndSessionsWithNotes(t *testing.T) {
	root := t.TempDir()
	o := pruneOpts{Dir: filepath.Join(root, "trace"), Store: filepath.Join(root, "obs"), Older: time.Hour}
	os.MkdirAll(o.Dir, 0o755)
	os.MkdirAll(o.Store, 0o755)
	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{"gone", "pinned", "noted"} {
		p := filepath.Join(o.Store, id+".json")
		os.WriteFile(p, []byte(`{"project":"/w"}`), 0o644)
		os.Chtimes(p, old, old)
	}
	w := watch.OpenWatched(o.Store, "")
	w.SetKeep("pinned", true)
	watch.OpenNotes(filepath.Join(o.Store, "notes")).Put("noted/1", watch.Note{Description: "why"})
	if _, err := prune(o); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"gone": false, "pinned": true, "noted": true} {
		if _, err := os.Stat(filepath.Join(o.Store, id+".json")); (err == nil) != want {
			t.Errorf("%s: exists=%v, want %v", id, err == nil, want)
		}
	}
}
