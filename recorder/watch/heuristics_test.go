package watch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHeuristics_RegisterRunAndCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script heuristic")
	}
	repo := editsRepo(t)
	h := OpenHeuristics(t.TempDir())
	script := filepath.Join(t.TempDir(), "guess")
	// proposes one good step and three it should refuse; checks it got the files on stdin
	os.WriteFile(script, []byte(`#!/bin/sh
grep -q '"path":"data/pk.csv"' || { echo "no file list on stdin" >&2; exit 3; }
echo '[{"inputs":["data/pk.csv"],"outputs":["report.html"],"cmd":"render"},
 {"inputs":[],"outputs":["../etc/passwd"],"cmd":"x"},
 {"inputs":["nope.csv"],"outputs":["report.html"],"cmd":"x"},
 {"inputs":[],"outputs":[],"cmd":"x"}]'
`), 0o755)
	for _, bad := range [][2]string{{"bad name", script}, {"rel", "guess"}, {"dir", t.TempDir()}} {
		if h.Register(bad[0], bad[1]) == nil {
			t.Errorf("registered %v", bad)
		}
	}
	if err := h.Register("guess", script); err != nil {
		t.Fatal(err)
	}
	ps, err := h.Run(context.Background(), "guess", repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 4 || ps[0].Refused != "" || ps[1].Refused == "" || ps[2].Refused == "" || ps[3].Refused == "" {
		t.Fatalf("proposals %+v", ps)
	}
	if _, err := h.Run(context.Background(), "unknown", repo); err == nil {
		t.Fatal("ran an unregistered heuristic")
	}
	// a heuristic that hangs is stopped
	slow := filepath.Join(t.TempDir(), "slow")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\necho '[]'\n"), 0o755)
	h.Register("slow", slow)
	h.Timeout = 200 * time.Millisecond
	if _, err := h.Run(context.Background(), "slow", repo); err == nil {
		t.Fatal("a hanging heuristic was not stopped")
	}
	// removing it
	if err := h.Register("guess", ""); err != nil || len(OpenHeuristics(filepath.Dir(h.path)).All()) != 1 {
		t.Fatalf("remove: %v %v", err, h.All())
	}
}
