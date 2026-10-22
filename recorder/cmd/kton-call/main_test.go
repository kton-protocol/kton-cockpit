package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

// Review K1 of #14: the exec line kton-call adds for a reuse is read back as exactly the
// arguments it was called with, spaces and quotes included, so the reuse is the same call.
func TestPutOutputs_TheReuseLineKeepsTheArguments(t *testing.T) {
	dir := t.TempDir()
	logf := filepath.Join(dir, "trace.tsv")
	os.WriteFile(logf, []byte("#ktontrace v3\thash="+dir+"\n"), 0o644)
	t.Setenv("KTON_TRACE_LOG", logf)
	t.Setenv("KTON_CAS", filepath.Join(dir, "cas"))
	args := os.Args
	defer func() { os.Args = args }()
	os.Args = []string{"/stubs/nmfe75", "run 1.mod", "it's.lst"}

	if err := putOutputs(watch.Call{Session: "s", Task: "1", Key: "k"}, dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logf)
	s := watch.NewSession("t", logf, &watch.Classifier{System: watch.DefaultSystem, Temp: watch.DefaultTemp})
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		s.Line(l)
	}
	s.Close("eof", 1<<62)
	var got []string
	for _, tk := range s.Tasks {
		for _, c := range tk.Commands {
			got = watch.SplitArgs(c.Cmdline)
		}
	}
	if len(got) != 3 || got[0] != "nmfe75" || got[1] != "run 1.mod" || got[2] != "it's.lst" {
		t.Fatalf("read back %q", got)
	}
}
