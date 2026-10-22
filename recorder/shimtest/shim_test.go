// Tests for libktontrace.so: each builds the shim and a small helper with gcc, runs one
// scenario with the shim preloaded, and checks the event log. Like the rest of the suite these
// do not skip: without gcc they fail, because a shim that is not built is not tested.
package shim_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

// lastLog is the log of the latest run, for a test that reads it with the watcher.
var lastLog string

type event struct {
	pid, op, mode, path, extra string
}

func (e event) kv(k string) string {
	for _, f := range strings.Split(e.extra, ";") {
		if v, ok := strings.CutPrefix(f, k+"="); ok {
			return v
		}
	}
	return ""
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

var built struct{ dir, lib, ops string }

func build(t *testing.T) {
	t.Helper()
	if built.lib != "" {
		return
	}
	dir, err := os.MkdirTemp("", "ktontrace-test-")
	if err != nil {
		t.Fatal(err)
	}
	built.dir = dir
	built.lib = filepath.Join(dir, "libktontrace.so")
	built.ops = filepath.Join(dir, "ops")
	for _, c := range [][]string{
		{"gcc", "-shared", "-fPIC", "-O2", "-Wall", "-Wno-format-truncation", "-o", built.lib, "../shim/ktontrace.c", "-ldl"},
		{"gcc", "-O2", "-o", built.ops, "testdata/ops.c"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			built.lib = ""
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

// run executes argv inside a fresh project dir with the shim preloaded and returns the events.
func run(t *testing.T, setup func(proj string), argv ...string) (string, []event) {
	t.Helper()
	build(t)
	proj, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(proj)
	}
	logf := filepath.Join(t.TempDir(), "trace.tsv")
	lastLog = logf
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = proj
	cmd.Env = append(os.Environ(), "LD_PRELOAD="+built.lib, "KTON_TRACE_LOG="+logf, "KTON_TRACE_HASH="+proj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", argv, err, out)
	}
	b, err := os.ReadFile(logf)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines[0] != "#ktontrace v3\thash="+proj {
		t.Fatalf("first line %q, want the format header naming the project", lines[0])
	}
	var evs []event
	for _, l := range lines[1:] {
		f := strings.SplitN(l, "\t", 10)
		if len(f) != 10 {
			t.Fatalf("malformed line %q", l)
		}
		evs = append(evs, event{pid: f[1], op: f[3], mode: f[4], path: f[5], extra: f[9]})
	}
	return proj, evs
}

func find(evs []event, op, path string) []event {
	var out []event
	for _, e := range evs {
		if e.op == op && e.path == path {
			out = append(out, e)
		}
	}
	return out
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShim_ReadIsHashedWhenOpened(t *testing.T) {
	proj, evs := run(t, func(p string) { write(t, filepath.Join(p, "in.csv"), "a,b\n1,2\n") }, opsPath(t), "read", "in.csv")
	o := find(evs, "open", filepath.Join(proj, "in.csv"))
	if len(o) != 1 || o[0].mode != "r" || o[0].kv("sha256") != sum("a,b\n1,2\n") {
		t.Fatalf("open of in.csv: %+v", o)
	}
}

// Review S1: a same-size rewrite that keeps the mtime used to hit the read's hash cache, and
// closew reported the old content.
func TestShim_ASameSizeRewriteIsHashedAfresh(t *testing.T) {
	proj, evs := run(t, func(p string) {
		f := filepath.Join(p, "data.csv")
		write(t, f, "aaaa")
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}, opsPath(t), "read", "data.csv", "rewritekeep", "data.csv", "bbbb")
	p := filepath.Join(proj, "data.csv")
	if o := find(evs, "open", p); len(o) < 1 || o[0].kv("sha256") != sum("aaaa") {
		t.Fatalf("open of data.csv: %+v", o)
	}
	if c := find(evs, "closew", p); len(c) != 1 || c[0].kv("sha256") != sum("bbbb") {
		t.Fatalf("closew must name the bytes written, not the cached read: %+v", c)
	}
}

// Review S2: a tab in a path used to become a space and name another file.
func TestShim_ATabInAPathIsEscaped(t *testing.T) {
	proj, evs := run(t, nil, opsPath(t), "write", "a\tb", "x")
	if c := find(evs, "closew", filepath.Join(proj, `a\tb`)); len(c) != 1 || c[0].kv("sha256") != sum("x") {
		t.Fatalf("closew of a<TAB>b, written as a\\tb: %+v", evs)
	}
}

func TestShim_ANewFileIsMarkedNewAndHashedAtClose(t *testing.T) {
	proj, evs := run(t, nil, opsPath(t), "write", "out.csv", "x\n")
	p := filepath.Join(proj, "out.csv")
	o := find(evs, "open", p)
	if len(o) != 1 || o[0].kv("new") != "1" || o[0].kv("sha256") != "" {
		t.Fatalf("a created file must be marked new and not hashed as an input: %+v", o)
	}
	c := find(evs, "closew", p)
	if len(c) != 1 || c[0].kv("sha256") != sum("x\n") {
		t.Fatalf("closew: %+v", c)
	}
}

// Regression: pread on a write-only fd fails with EBADF, and closew used to vanish.
func TestShim_WriteOnlyFdIsStillHashedAtClose(t *testing.T) {
	proj, evs := run(t, nil, opsPath(t), "writeonly", "w.txt", "hello", "fopenw", "f.txt", "fopen")
	if c := find(evs, "closew", filepath.Join(proj, "w.txt")); len(c) != 1 || c[0].kv("sha256") != sum("hello") {
		t.Fatalf("closew w.txt: %+v", c)
	}
	if c := find(evs, "closew", filepath.Join(proj, "f.txt")); len(c) != 1 || c[0].kv("sha256") != sum("fopen") {
		t.Fatalf("closew f.txt: %+v", c)
	}
}

func TestShim_TruncatingAnExistingFileIsNotAnInput(t *testing.T) {
	proj, evs := run(t, func(p string) { write(t, filepath.Join(p, "t.txt"), "old") }, opsPath(t), "writeonly", "t.txt", "new")
	o := find(evs, "open", filepath.Join(proj, "t.txt"))
	if len(o) != 1 || o[0].kv("trunc") != "1" || o[0].kv("new") != "" {
		t.Fatalf("open: %+v", o)
	}
}

// The last close of a dup'd fd reports the content; the first close happened before the write.
func TestShim_DupKeepsTracking(t *testing.T) {
	proj, evs := run(t, nil, opsPath(t), "dupwrite", "d.txt", "via dup")
	c := find(evs, "closew", filepath.Join(proj, "d.txt"))
	if len(c) == 0 || c[len(c)-1].kv("sha256") != sum("via dup") {
		t.Fatalf("closew d.txt: %+v", c)
	}
}

// A shell's `> file`: the shell opens and closes the file empty, the program it starts writes
// into its stdout and never closes it. The exiting child reports the final content.
func TestShim_ShellRedirectReportsFinalContent(t *testing.T) {
	proj, evs := run(t, func(p string) { write(t, filepath.Join(p, "in.txt"), "line\n") }, "/bin/sh", "-c", "cat in.txt > out.txt")
	p := filepath.Join(proj, "out.txt")
	if inh := find(evs, "inherit", p); len(inh) != 1 {
		t.Fatalf("cat should report the inherited write fd: %+v", inh)
	}
	c := find(evs, "closew", p)
	if len(c) == 0 || c[len(c)-1].kv("sha256") != sum("line\n") {
		t.Fatalf("the last closew must carry the final content: %+v", c)
	}
}

func TestShim_RenameAndUnlinkUseCanonicalPaths(t *testing.T) {
	proj, evs := run(t, func(p string) {
		if err := os.Mkdir(filepath.Join(p, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(p, "link")); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(p, "real", "a.tmp"), "x")
		write(t, filepath.Join(p, "gone.txt"), "y")
	}, opsPath(t), "rename", "link/a.tmp", "link/a.txt", "unlink", "gone.txt")
	r := find(evs, "rename", filepath.Join(proj, "real", "a.txt"))
	if len(r) != 1 || r[0].extra != filepath.Join(proj, "real", "a.tmp") {
		t.Fatalf("rename through a symlinked dir must log resolved paths: %+v", evs)
	}
	if u := find(evs, "unlink", filepath.Join(proj, "gone.txt")); len(u) != 1 {
		t.Fatalf("unlink: %+v", evs)
	}
}

func TestShim_AMissingInputIsLoggedAsOpenfail(t *testing.T) {
	proj, evs := run(t, nil, opsPath(t), "openfail", "missing.csv")
	if f := find(evs, "openfail", filepath.Join(proj, "missing.csv")); len(f) != 1 || f[0].kv("errno") != "2" {
		t.Fatalf("openfail: %+v", evs)
	}
}

func TestShim_LoopbackIsClassified(t *testing.T) {
	_, evs := run(t, nil, opsPath(t), "connect", "9")
	for _, e := range evs {
		if e.op == "connect" && strings.HasPrefix(e.path, "inet:127.0.0.1:") {
			if e.kv("class") != "loopback" {
				t.Fatalf("class: %+v", e)
			}
			return
		}
	}
	t.Fatalf("no connect event: %+v", evs)
}

// A statically linked program never loads the shim; the spawn line is all that is left of it.
func TestShim_SpawnIsLoggedEvenForAStaticProgram(t *testing.T) {
	build(t)
	static := filepath.Join(built.dir, "static")
	if out, err := exec.Command("gcc", "-static", "-O2", "-o", static, "testdata/ops.c").CombinedOutput(); err != nil {
		t.Fatalf("gcc -static: %v\n%s", err, out)
	}
	_, evs := run(t, nil, "/bin/sh", "-c", static+" read nothing")
	var spawnPid string
	for _, e := range evs {
		if e.op == "spawn" && e.path == static {
			spawnPid = e.pid
		}
		if spawnPid != "" && e.op == "exec" && e.pid == spawnPid {
			t.Fatalf("a static program cannot log its own exec: %+v", evs)
		}
	}
	if spawnPid == "" {
		t.Fatalf("no spawn for %s: %+v", static, evs)
	}
}

func opsPath(t *testing.T) string {
	build(t)
	return built.ops
}

// %p gives the first process its own log; its children write into the same one.
func TestShim_PidInLogNameIsExpandedOnce(t *testing.T) {
	build(t)
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", "cat /dev/null")
	cmd.Env = append(os.Environ(), "LD_PRELOAD="+built.lib, "KTON_TRACE_LOG="+filepath.Join(dir, "r-%p.tsv"), "KTON_TRACE_HASH="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	logs, _ := filepath.Glob(filepath.Join(dir, "r-*.tsv"))
	if len(logs) != 1 {
		t.Fatalf("want one log for sh and its child cat, got %v", logs)
	}
	b, _ := os.ReadFile(logs[0])
	if n := strings.Count(string(b), "\texec\t"); n != 2 {
		t.Fatalf("want sh and cat in the same log, %d exec lines:\n%s", n, b)
	}
}

// While PAUSED exists next to the log, the shim writes nothing (the switch in Positron).
func TestShim_PausedLogsNothing(t *testing.T) {
	build(t)
	dir := t.TempDir()
	logf := filepath.Join(dir, "trace.tsv")
	if err := os.WriteFile(filepath.Join(dir, "PAUSED"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(built.ops, "write", filepath.Join(dir, "x.txt"), "y")
	cmd.Env = append(os.Environ(), "LD_PRELOAD="+built.lib, "KTON_TRACE_LOG="+logf, "KTON_TRACE_HASH="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ := os.ReadFile(logf)
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 1 {
		t.Fatalf("only the header may be written while paused:\n%s", b)
	}
}

// Review R2 of #12: an argument with a space stays one argument in the exec line.
func TestShim_AnArgumentWithASpaceIsQuotedInTheExecLine(t *testing.T) {
	_, evs := run(t, nil, opsPath(t), "write", "run 1.lst", "x", "write", "it's", "y")
	for _, e := range evs {
		if e.op == "exec" {
			if e.mode != "q" {
				t.Fatalf("exec line in mode %q, want q (quoted)", e.mode)
			}
			// as logged: the v2 escaping doubles the backslash of '\''
			if !strings.HasSuffix(e.extra, ` write 'run 1.lst' x write 'it'\\''s' y`) {
				t.Fatalf("exec line %q", e.extra)
			}
			return
		}
	}
	t.Fatalf("no exec line: %+v", evs)
}

// The Python console's command boundaries (recorder/python/ktonrec.py): under the shim, each
// execution becomes a task with the files it touched, and the session says it is Python.
func TestShim_PythonConsoleCellsBecomeTasks(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	py, _ := filepath.Abs("../python")
	fake, _ := filepath.Abs("testdata/ipython_fake.py")
	proj, _ := run(t, func(p string) { write(t, filepath.Join(p, "in.csv"), "a\n") }, "python3", "-I", "-B", fake, py)
	logs, _ := filepath.Glob(filepath.Join(filepath.Dir(lastLog), "*.tsv"))
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	b, _ := os.ReadFile(logs[0])
	s := watch.NewSession("t", logs[0], &watch.Classifier{System: watch.DefaultSystem, Temp: watch.DefaultTemp})
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		s.Line(l)
	}
	if s.Language != "python" || s.Project != proj || len(s.Tasks) < 2 {
		t.Fatalf("session lang %q project %q tasks %d", s.Language, s.Project, len(s.Tasks))
	}
	t1, t2 := s.Tasks[0], s.Tasks[1]
	if t1.Expr != `%run -- "step1.py"` || len(t1.Inputs) != 1 || filepath.Base(t1.Inputs[0].Path) != "in.csv" ||
		len(t1.Outputs) != 1 || filepath.Base(t1.Outputs[0].Path) != "mid.csv" {
		t.Fatalf("cell 1: %q in %v out %v", t1.Expr, inPaths(t1.Inputs), outPaths(t1.Outputs))
	}
	if len(t2.Inputs) != 1 || filepath.Base(t2.Inputs[0].Path) != "mid.csv" || len(t2.Inputs[0].From) != 1 ||
		len(t2.Outputs) != 1 || filepath.Base(t2.Outputs[0].Path) != "out.csv" {
		t.Fatalf("cell 2: in %v out %v", inPaths(t2.Inputs), outPaths(t2.Outputs))
	}
}

func inPaths(xs []*watch.Input) (out []string) {
	for _, x := range xs {
		out = append(out, x.Path)
	}
	return out
}

func outPaths(xs []*watch.Output) (out []string) {
	for _, x := range xs {
		out = append(out, x.Path)
	}
	return out
}

// Review P2, P3, P5 of #15: the startup file starts the boundaries only under the shim, writes
// no bytecode of ktonrec while doing so, stays valid Python with a '"' in the installed path,
// and a missing log costs the console nothing.
func TestShim_TheIPythonStartupFile(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	startup, _ := filepath.Abs("../python/50-kton.py")
	fake, _ := filepath.Abs("testdata/startup_fake.py")
	inst := filepath.Join(t.TempDir(), `pre"fix`, "python") // a quote in $PREFIX
	os.MkdirAll(inst, 0o755)
	src, _ := os.ReadFile("../python/ktonrec.py")
	os.WriteFile(filepath.Join(inst, "ktonrec.py"), src, 0o644)

	_, evs := run(t, nil, "python3", "-I", fake, startup, inst, "expect-start")
	var env, tasks int
	for _, e := range evs {
		switch e.op {
		case "ENV":
			env++
		case "TASK":
			tasks++
		}
	}
	if env != 1 || tasks != 1 {
		t.Fatalf("ENV %d TASK %d: one cell while the log was there, none while it was away", env, tasks)
	}
	if _, err := os.Stat(filepath.Join(inst, "__pycache__")); err == nil {
		t.Fatalf("ktonrec's bytecode was written at import")
	}
	cmd := exec.Command("python3", "-I", fake, startup, inst, "expect-nothing")
	cmd.Env = append(os.Environ(), "KTON_TRACE_LOG=", "LD_PRELOAD=")
	if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
		t.Fatalf("outside the shim it must do nothing and say nothing: %v %s", err, out)
	}
}
