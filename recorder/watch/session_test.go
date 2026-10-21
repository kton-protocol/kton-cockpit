package watch

import (
	"fmt"
	"strings"
	"testing"
)

const proj = "/p"

// logb builds a log line by line; ts advances by one per line.
type logb struct {
	lines []string
	ts    int64
}

func (l *logb) ev(pid int, op, mode, path, extra string) *logb {
	l.ts++
	if extra == "" {
		extra = "-"
	}
	l.lines = append(l.lines, fmt.Sprintf("%d\t%d\t1\t%s\t%s\t%s\t0\t0\t0\t%s", l.ts, pid, op, mode, path, extra))
	return l
}
func (l *logb) task(n int, status, expr string) *logb {
	return l.ev(100, "TASK", status, fmt.Sprint(n), expr)
}

func sess(t *testing.T, l *logb, files map[string]string) *Session {
	t.Helper()
	cls := &Classifier{System: DefaultSystem, Temp: DefaultTemp, Env: []string{"/usr/lib/R/"}}
	s := NewSession("t", "/log.tsv", cls)
	s.Hash = func(p string) (string, string) {
		if h, ok := files[p]; ok {
			return h, "hashed-by-watcher"
		}
		return "", "gone"
	}
	s.Line("#ktontrace v1\thash=" + proj)
	for _, line := range l.lines {
		s.Line(line)
	}
	return s
}

func task(t *testing.T, s *Session, id string) *Task {
	t.Helper()
	for _, x := range s.Tasks {
		if x.ID == id {
			return x
		}
	}
	t.Fatalf("no task %q in %+v", id, s.Tasks)
	return nil
}

func TestSession_HeaderNamesTheProject(t *testing.T) {
	s := sess(t, &logb{}, nil)
	if s.Project != proj {
		t.Fatalf("project %q", s.Project)
	}
}

func TestSession_InputOutputAndProvenance(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "r", "/p/raw.csv", "sha256=aaa").
		ev(100, "open", "w", "/p/clean.csv", "new=1").
		ev(100, "closew", "w", "/p/clean.csv", "sha256=bbb").
		task(1, "ok", "clean <- …").
		ev(100, "open", "r", "/p/clean.csv", "sha256=bbb").
		task(2, "ok", "read clean")
	s := sess(t, l, nil)
	t1, t2 := task(t, s, "1"), task(t, s, "2")
	if len(t1.Inputs) != 1 || t1.Inputs[0].SHA256 != "aaa" || len(t1.Outputs) != 1 || t1.Outputs[0].SHA256 != "bbb" {
		t.Fatalf("task 1: in %+v out %+v", t1.Inputs, t1.Outputs)
	}
	if in := t2.Inputs[0]; len(in.From) != 1 || in.From[0] != (Ref{Task: "1", Path: "/p/clean.csv"}) {
		t.Fatalf("task 2 input should come from task 1: %+v", in)
	}
}

// Fortran opens without ACTION= as read-write and creates files: neither is an input (S3).
func TestSession_NewAndTruncatedFilesAreNotInputs(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "rw", "/p/FCON", "new=1").
		ev(100, "open", "rw", "/p/old.lst", "trunc=1").
		task(1, "ok", "nmfe")
	if in := task(t, sess(t, l, map[string]string{"/p/FCON": EmptySHA256, "/p/old.lst": EmptySHA256}), "1").Inputs; len(in) != 0 {
		t.Fatalf("inputs %+v", in)
	}
}

// A control stream opened read-write and left alone is an input, not an output (S3).
func TestSession_ReadWriteUnchangedIsNotAnOutput(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "rw", "/p/run1.mod", "sha256=mmm").
		ev(100, "closew", "w", "/p/run1.mod", "sha256=mmm").
		task(1, "ok", "nmfe")
	t1 := task(t, sess(t, l, nil), "1")
	if len(t1.Inputs) != 1 || len(t1.Outputs) != 0 {
		t.Fatalf("in %+v out %+v", t1.Inputs, t1.Outputs)
	}
}

func TestSession_TempFileRenamedIntoPlace(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "w", "/p/.x.tmp", "new=1").
		ev(100, "closew", "w", "/p/.x.tmp", "sha256=xxx").
		ev(100, "rename", "-", "/p/x.rds", "/p/.x.tmp").
		ev(100, "open", "w", "/p/scratch", "new=1").
		ev(100, "unlink", "-", "/p/scratch", "").
		task(1, "ok", "saveRDS")
	t1 := task(t, sess(t, l, nil), "1")
	if len(t1.Outputs) != 1 || t1.Outputs[0].Path != "/p/x.rds" || t1.Outputs[0].SHA256 != "xxx" {
		t.Fatalf("outputs %+v", t1.Outputs)
	}
	if strings.Join(t1.Temporary, ",") != "/p/.x.tmp,/p/scratch" {
		t.Fatalf("temporary %v", t1.Temporary)
	}
}

// Regression (S3): the next command renamed the file before the watcher looked; the output
// must come from the events, not from the file system.
func TestSession_OutputSurvivesARenameInTheNextTask(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "w", "/p/run1.lst", "new=1").
		ev(100, "closew", "w", "/p/run1.lst", "sha256=lst").
		task(1, "ok", "system('nmfe')").
		ev(100, "rename", "-", "/p/run1_system.lst", "/p/run1.lst").
		task(2, "ok", "file.rename(…)")
	s := sess(t, l, nil) // no file exists on "disk"
	if o := task(t, s, "1").Outputs; len(o) != 1 || o[0].SHA256 != "lst" {
		t.Fatalf("task 1 outputs %+v", o)
	}
	if o := task(t, s, "2").Outputs; len(o) != 1 || o[0].Path != "/p/run1_system.lst" || o[0].SHA256 != "lst" {
		t.Fatalf("task 2 outputs %+v", o)
	}
}

// pharmpy copies the dataset into its database; reading the original afterwards is not a read
// of the copy (S3, pharmpy).
func TestSession_SameBytesElsewhereIsNotProvenance(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "r", "/p/THEOPP", "sha256=ddd").
		ev(100, "open", "w", "/p/modelfit1/.datasets/THEOPP", "new=1").
		ev(100, "closew", "w", "/p/modelfit1/.datasets/THEOPP", "sha256=ddd").
		task(1, "ok", "fit").
		ev(100, "open", "r", "/p/THEOPP", "sha256=ddd").
		task(2, "ok", "derive")
	in := task(t, sess(t, l, nil), "2").Inputs[0]
	if len(in.From) != 0 || len(in.SameAs) != 1 || in.SameAs[0].Path != "/p/modelfit1/.datasets/THEOPP" {
		t.Fatalf("input %+v", in)
	}
}

func TestSession_ChangedWithoutARecordedWriter(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "w", "/p/a.csv", "new=1").
		ev(100, "closew", "w", "/p/a.csv", "sha256=v1").
		task(1, "ok", "write").
		ev(100, "open", "r", "/p/a.csv", "sha256=v2").
		task(2, "ok", "read")
	if in := task(t, sess(t, l, nil), "2").Inputs[0]; in.Status != "changed-outside" || len(in.From) != 0 {
		t.Fatalf("input %+v", in)
	}
}

func TestSession_UnrecordedProgramsAndNetwork(t *testing.T) {
	l := (&logb{}).
		ev(200, "spawn", "-", "/p/static", "argv=/p/static x").
		ev(201, "spawn", "-", "/usr/bin/cat", "child=201;argv=cat a").
		ev(201, "exec", "-", "/p", "cat a").
		ev(100, "connect", "net", "inet:127.0.0.1:41731", "class=loopback;rc=-1;errno=115").
		ev(100, "connect", "net", "inet:192.0.2.1:443", "rc=-1 errno=115").
		task(1, "ok", "x")
	t1 := task(t, sess(t, l, nil), "1")
	if len(t1.Unrecorded) != 1 || t1.Unrecorded[0].Program != "/p/static" {
		t.Fatalf("unrecorded %+v", t1.Unrecorded)
	}
	if len(t1.Network) != 2 || t1.Network[0].Class != "loopback" || t1.Network[1].Class != "remote" {
		t.Fatalf("network %+v", t1.Network)
	}
}

func TestSession_SourceEnvMissingAndTail(t *testing.T) {
	l := (&logb{}).
		ev(100, "ENV", "-", "/opt/nm751", "").
		ev(100, "open", "r", "/opt/nm751/util/x", "").
		ev(100, "SOURCE", "-", "/p/step2.R", "").
		ev(100, "SOURCE", "-", "/usr/lib/R/library/tools/R/tools", "").
		ev(100, "openfail", "r", "/p/gibtsnicht.csv", "errno=2").
		task(1, "err", "source('step2.R')").
		ev(100, "open", "r", "/p/late.csv", "sha256=lll")
	s := sess(t, l, nil)
	t1 := task(t, s, "1")
	if t1.EnvFiles != 1 || len(t1.Inputs) != 0 {
		t.Fatalf("ENV prefix not applied: env %d inputs %+v", t1.EnvFiles, t1.Inputs)
	}
	if len(t1.Sources) != 2 || t1.Sources[0].Class != ClassProject || t1.Sources[1].Class != ClassEnv {
		t.Fatalf("sources %+v", t1.Sources)
	}
	if len(t1.Missing) != 1 || t1.Status != "err" {
		t.Fatalf("missing %v status %s", t1.Missing, t1.Status)
	}
	s.Close("timeout", 999)
	if tail := task(t, s, "tail"); len(tail.Inputs) != 1 || !s.Closed || s.CloseReason != "timeout" {
		t.Fatalf("tail %+v closed %v", tail, s.Closed)
	}
}

// R reads its own libraries before ktonrec can say where R lives; the ENV line reclassifies
// what the current command read so far.
func TestSession_EnvLineReclassifiesEarlierReads(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "r", "/opt/R/library/base/R/base.rdb", "").
		ev(100, "open", "r", "/p/script.R", "sha256=s").
		ev(100, "ENV", "-", "/opt/R,/home/u/R/library", "").
		task(1, "ok", "start")
	t1 := task(t, sess(t, l, map[string]string{"/opt/R/library/base/R/base.rdb": "b"}), "1")
	if len(t1.Inputs) != 1 || t1.Inputs[0].Path != "/p/script.R" || t1.EnvFiles != 1 {
		t.Fatalf("inputs %+v env %d", t1.Inputs, t1.EnvFiles)
	}
}

func TestSession_EnvLineNamesTheProject(t *testing.T) {
	l := (&logb{}).
		ev(100, "ENV", "-", "/opt/R", "project=/p/sub").
		ev(100, "open", "r", "/p/sub/a.csv", "sha256=a").
		ev(100, "open", "r", "/p/other.csv", "sha256=b").
		task(1, "ok", "x")
	s := sess(t, l, nil)
	t1 := task(t, s, "1")
	if s.Project != "/p/sub" || t1.Inputs[0].Class != ClassProject || t1.Inputs[1].Class != ClassOutside {
		t.Fatalf("project %q inputs %+v", s.Project, t1.Inputs)
	}
}

// A NONMEM rerun reads the scratch files the previous run left, then rewrites them; and bash
// reads the nmfe75 script it runs. Neither is data.
func TestSession_ScratchFilesAndScriptsAreNotData(t *testing.T) {
	l := (&logb{}).
		ev(101, "exec", "-", "/p", "/bin/bash /opt/nm/run/nmfe75 run1.ctl run1.lst").
		ev(101, "open", "r", "/opt/nm/run/nmfe75", "").
		ev(101, "open", "r", "/p/run1.ctl", "sha256=ctl").
		ev(102, "open", "rw", "/p/FCON", "sha256=old").
		ev(102, "closew", "w", "/p/FCON", "sha256=new").
		task(1, "ok", "system('nmfe75 …')")
	t1 := task(t, sess(t, l, map[string]string{"/opt/nm/run/nmfe75": "script"}), "1")
	if len(t1.Inputs) != 2 || t1.Inputs[0].Path != "/p/run1.ctl" || t1.Inputs[0].Rewritten || !t1.Inputs[1].Rewritten {
		t.Fatalf("inputs %+v", t1.Inputs)
	}
}

// A rerun of the same model writes identical bytes; the changed mtime still makes it an output,
// and a work file deleted and recreated is not an input.
func TestSession_RerunWithIdenticalBytes(t *testing.T) {
	l := &logb{}
	l.lines = append(l.lines,
		"1\t100\t1\topen\trw\t/p/run1.ext\t10\t1000\t0\tsha256=ext",
		"3\t100\t1\tclosew\tw\t/p/run1.ext\t10\t2000\t0\tsha256=ext",
		"4\t100\t1\topen\tr\t/p/FCON\t5\t500\t0\tsha256=fcon",
		"5\t100\t1\tunlink\t-\t/p/FCON\t0\t0\t0\t-",
		"6\t100\t1\topen\tw\t/p/FCON\t0\t3000\t0\tnew=1",
		"7\t100\t1\tclosew\tw\t/p/FCON\t5\t3000\t0\tsha256=fcon",
		"8\t100\t1\tTASK\tok\t1\t0\t0\t0\tnmfe")
	t1 := task(t, sess(t, l, nil), "1")
	outs := map[string]bool{}
	for _, o := range t1.Outputs {
		outs[o.Path] = true
	}
	if !outs["/p/run1.ext"] {
		t.Fatalf("run1.ext was written (mtime changed): outputs %+v", t1.Outputs)
	}
	for _, in := range t1.Inputs {
		if !in.Rewritten {
			t.Fatalf("both inputs were rewritten by the task: %+v", in)
		}
	}
}

// PsN (and callr) start an R underneath; its markers must not split or re-home the session.
func TestSession_MarkersOfANestedRAreIgnored(t *testing.T) {
	l := (&logb{}).
		ev(100, "ENV", "-", "/usr/lib/R", "project=/p").
		ev(100, "open", "r", "/p/a.csv", "sha256=a").
		ev(200, "exec", "-", "/p/model/run2_psn", "/usr/lib/R/bin/exec/R --vanilla").
		ev(200, "ENV", "-", "/usr/lib/R", "project=/p/model/run2_psn").
		ev(200, "TASK", "ok", "1", "nested").
		ev(200, "open", "r", "/p/model/x.csv", "sha256=x").
		task(1, "ok", "system('execute run2.mod')")
	s := sess(t, l, nil)
	if s.Project != "/p" || len(s.Tasks) != 1 || len(s.Tasks[0].Inputs) != 2 {
		t.Fatalf("project %q tasks %+v", s.Project, s.Tasks)
	}
}

// Regression (working day): the watcher read the log after the file was edited by hand. The
// output is what R wrote (closew), never what is on disk by then.
func TestSession_LateReadDoesNotTakeAHandEditAsOutput(t *testing.T) {
	l := (&logb{}).
		ev(100, "open", "w", "/p/analysis.csv", "new=1").
		ev(100, "closew", "w", "/p/analysis.csv", "sha256=written-by-r").
		task(1, "ok", "write.csv(…)")
	s := sess(t, l, map[string]string{"/p/analysis.csv": "edited-in-excel"})
	if o := task(t, s, "1").Outputs; len(o) != 1 || o[0].SHA256 != "written-by-r" {
		t.Fatalf("outputs %+v", o)
	}
}

func TestFilter_ToolProbesAreNotMissingInputs(t *testing.T) {
	for p, want := range map[string]bool{"/p/model/sizes.mod": true, "/p/model/psn.phm": true, "/p/stdin": true,
		"/p/model/run2.mod": false, "/p/data/pk.csv": false} {
		if probe(p) != want {
			t.Fatalf("probe(%s) = %v", p, !want)
		}
	}
}

func TestSession_RecognisesAnRScriptSession(t *testing.T) {
	for cmd, want := range map[string]string{
		"/usr/lib/R/bin/exec/R --no-save -q -f R/01_daten.R":           "/p/R/01_daten.R",
		"/usr/lib/R/bin/exec/R --no-echo --no-restore --file=/abs/x.R": "/abs/x.R",
		"/usr/lib/R/bin/exec/R --no-save -q":                           "",
		"/bin/bash /usr/bin/R -f x.R":                                  "",
	} {
		if got := rScriptArg(cmd, "/p"); got != want {
			t.Fatalf("%q: %q, want %q", cmd, got, want)
		}
	}
	l := (&logb{}).
		ev(100, "exec", "-", "/p", "/usr/lib/R/bin/exec/R --no-save -q -f R/01_daten.R").
		ev(100, "open", "r", "/p/R/01_daten.R", "sha256=scr").
		task(1, "ok", "x <- 1").
		task(2, "ok", "y <- 2")
	s := sess(t, l, nil)
	if s.Script != "/p/R/01_daten.R" || s.ScriptSHA != "scr" || s.Tasks[1].Script != "/p/R/01_daten.R" {
		t.Fatalf("script %q %q tasks %+v", s.Script, s.ScriptSHA, s.Tasks[1])
	}
}

// The Claude Code hook marks each Bash command from a new process that is not traced itself;
// all of those markers belong to the session.
func TestSession_MarkersFromUntracedProcessesCount(t *testing.T) {
	l := (&logb{}).
		ev(300, "exec", "-", "/p", "ls -la").
		ev(301, "TASK", "ok", "1", "ls -la").
		ev(302, "exec", "-", "/p", "Rscript R/01.R").
		ev(302, "open", "w", "/p/a.csv", "new=1").
		ev(302, "closew", "w", "/p/a.csv", "sha256=a").
		ev(303, "TASK", "ok", "2", "Rscript R/01.R").
		ev(304, "exec", "-", "/p", "nmfe75 run1.mod run1.lst").
		ev(305, "TASK", "err", "3", "nmfe75 run1.mod run1.lst")
	s := sess(t, l, nil)
	if len(s.Tasks) != 3 || len(task(t, s, "2").Outputs) != 1 || task(t, s, "3").Status != "err" {
		t.Fatalf("tasks %+v", s.Tasks)
	}
}

// A file a command looks for and then writes (an existence check, NONMEM's optional files)
// is not missing; shared-object lookups never are inputs.
func TestSession_LookedForThenWrittenIsNotMissing(t *testing.T) {
	l := (&logb{}).
		ev(100, "openfail", "r", "/p/data/nca.csv", "").
		ev(100, "open", "w", "/p/data/nca.csv", "new=1").
		ev(100, "closew", "w", "/p/data/nca.csv", "sha256=n").
		ev(100, "openfail", "r", "/p/data/gone.csv", "").
		task(1, "ok", "nca()")
	if m := task(t, sess(t, l, nil), "1").Missing; len(m) != 1 || m[0] != "/p/data/gone.csv" {
		t.Fatalf("missing %v", m)
	}
	for _, p := range []string{"/p/models/linux-vdso.so.1", "/p/models/nmprd_char.mod"} {
		if !probe(p) {
			t.Fatalf("%s is a probe", p)
		}
	}
}

// pandoc (static, unseen) rewrites the report after Quarto's last write the shim saw, before the
// command ends: the file on disk is the output. A change after the end stays a hand edit.
func TestSession_UnseenRewriteWithinTheCommandIsTheOutput(t *testing.T) {
	old := fileMtime
	defer func() { fileMtime = old }()
	for _, tc := range []struct {
		mtime int64
		want  string
	}{{2, "final"}, {50, "quarto"}} { // the command ends at ts 3 (logb counts up)
		fileMtime = func(string) (int64, bool) { return tc.mtime, true }
		l := (&logb{}).
			ev(100, "open", "w", "/p/report.html", "trunc=1").
			ev(100, "closew", "w", "/p/report.html", "sha256=quarto").
			task(1, "ok", "quarto render")
		s := sess(t, l, map[string]string{"/p/report.html": "final"})
		if o := task(t, s, "1").Outputs; len(o) != 1 || o[0].SHA256 != tc.want {
			t.Fatalf("mtime %d: %+v", tc.mtime, o)
		}
	}
}
