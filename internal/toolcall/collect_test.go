package toolcall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The NONMEM collector takes the control stream and what it reads, in the usual layout where a run
// directory names its data two levels up; $INCLUDE is followed; a comment is not a record; the
// listing it writes is not an input.
func TestCollect_NonmemTakesTheControlStreamAndWhatItReads(t *testing.T) {
	root := t.TempDir()
	write(t, root, "data/pk.csv", "ID,TIME,DV\n")
	write(t, root, "models/run1/run1.mod", "$PROBLEM pk\n$INPUT ID TIME DV\n$DATA ../../data/pk.csv IGNORE=@\n"+
		"$INCLUDE priors.inc ; shared priors\n; $DATA not-a-record.csv\n$ESTIMATION METHOD=1\n")
	write(t, root, "models/run1/priors.inc", "$MSFI run0.msf\n")
	write(t, root, "models/run1/run0.msf", "msf")
	got, err := Collect("nonmem", root, "models/run1", []string{"run1.mod", "run1.lst"})
	if err != nil {
		t.Fatal(err)
	}
	want := "models/run1/run1.mod data/pk.csv models/run1/priors.inc models/run1/run0.msf"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " "), want)
	}

	write(t, root, "models/run2/run2.mod", "$DATA /home/someone/pk.csv\n")
	if _, err := Collect("nonmem", root, "models/run2", []string{"run2.mod"}); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Errorf("data outside the repository: want it refused, got %v", err)
	}
	write(t, root, "models/run3/run3.mod", "$DATA ../../../../etc/passwd\n")
	if _, err := Collect("nonmem", root, "models/run3", []string{"run3.mod"}); err == nil {
		t.Errorf("data above the repository: want it refused")
	}
	write(t, root, "models/run5/run5.mod", "$INCLUDE run5.mod\n")
	if _, err := Collect("nonmem", root, "models/run5", []string{"run5.mod"}); err == nil || !strings.Contains(err.Error(), "includes itself") {
		t.Errorf("a control stream that includes itself: want it refused, got %v", err)
	}
	write(t, root, "models/run4/run4.mod", "$DATA missing.csv\n")
	if _, err := Collect("nonmem", root, "models/run4", []string{"run4.mod"}); err == nil || !strings.Contains(err.Error(), "not there") {
		t.Errorf("data that is not there: want it said, got %v", err)
	}
}

// The files collector takes every argument that names a regular file of the repository, also as
// an option's value; anything else is an argument only.
func TestCollect_FilesTakesTheArgumentsThatAreFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "data/in.csv", "x")
	write(t, root, "models/opts.txt", "y")
	got, err := Collect("files", root, "models", []string{"../data/in.csv", "out.csv", "--options=opts.txt", "-v"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "data/in.csv models/opts.txt" {
		t.Errorf("got %v", got)
	}
}
