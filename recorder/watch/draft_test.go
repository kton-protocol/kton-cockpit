package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The whole point of a draft: what the viewer proposes is accepted by `cockpit publish` typed
// by hand, in a real participant repo. Documenting an observation is the manual verb, nothing
// else.
func TestDraft_IsAcceptedByCockpitPublishAtTheCommandLine(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "data/in.csv", "id,x\n1,2\n")
	r.Write(t, "data/out.csv", "id,x,y\n1,2,3\n")
	root, _ := filepath.EvalSymlinks(r.Root)

	l := (&logb{}).
		ev(100, "ENV", "-", "/usr/lib/R", "project="+root).
		ev(100, "SOURCE", "-", root+"/step.R", "").
		ev(100, "open", "r", root+"/data/in.csv", "sha256=in").
		ev(100, "open", "r", "/elsewhere/cov.csv", "sha256=cov").
		ev(100, "open", "w", root+"/data/out.csv", "new=1").
		ev(100, "closew", "w", root+"/data/out.csv", "sha256=out").
		task(1, "ok", `source("step.R")`)
	s := sess(t, l, nil)
	d := MakeDraft(s, task(t, s, "1"))

	if d.Repo != root || d.Cmd != `source("step.R")` {
		t.Fatalf("draft %+v", d)
	}
	req := map[string]any{"cmd": d.Cmd}
	var ins, outs []string
	for _, f := range d.Inputs {
		if f.Use {
			ins = append(ins, f.Path)
		} else if !strings.Contains(f.Note, "not in the repo") {
			t.Fatalf("only outside files may be left out: %+v", f)
		}
	}
	for _, f := range d.Outputs {
		if f.Use {
			outs = append(outs, f.Path)
		}
	}
	if strings.Join(ins, ",") != "data/in.csv" || strings.Join(outs, ",") != "data/out.csv" {
		t.Fatalf("inputs %v outputs %v", ins, outs)
	}
	req["inputs"], req["outputs"] = ins, outs
	b, _ := json.Marshal(req)
	out, code := r.Cockpit(t, "publish", string(b))
	if code != 0 || !strings.Contains(out, "sha256:") {
		t.Fatalf("cockpit publish %s exited %d:\n%s", b, code, out)
	}
}

func TestChecker_SameChangedGoneAndDraftDeselects(t *testing.T) {
	dir := t.TempDir()
	same, changed := filepath.Join(dir, "a.csv"), filepath.Join(dir, "b.csv")
	writeFile(t, same, "a")
	writeFile(t, changed, "b-new")
	c := NewChecker()
	ta := c.Annotate([]*Task{{
		Inputs:  []*Input{{Path: same, SHA256: sha("a"), Class: ClassProject}, {Path: changed, SHA256: sha("b-old"), Class: ClassProject}},
		Outputs: []*Output{{Path: filepath.Join(dir, "gone.csv"), SHA256: sha("x"), Class: ClassProject}},
	}})[0]
	if ta.Inputs[0].Now != NowSame || ta.Inputs[1].Now != NowChanged || ta.Outputs[0].Now != NowGone {
		t.Fatalf("now: %s %s %s", ta.Inputs[0].Now, ta.Inputs[1].Now, ta.Outputs[0].Now)
	}
	s := NewSession("x", "", &Classifier{Project: dir})
	d := MakeDraft(s, ta)
	if !d.Inputs[0].Use || d.Inputs[1].Use || d.Outputs[0].Use {
		t.Fatalf("only the unchanged file may be preselected: %+v %+v", d.Inputs, d.Outputs)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// A shell script, no R and so no markers: the session is one "tail" task. Its redirect
// (`awk … > out.csv`) is opened by the shell and inherited by awk; that file is the output, not
// the session's console. The draft names the script as the command, and cockpit publish takes it.
func TestDraft_AShellScriptWithoutMarkersIsPublishedWithItsOutput(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "means.sh", "#!/bin/sh\nawk -F, 'NR>1{s+=$2;n++} END{print \"mean,\" s/n}' data/pk.csv > out.csv\n")
	r.Write(t, "data/pk.csv", "id,v\n1,2\n2,4\n")
	r.Write(t, "out.csv", "mean,3\n")
	root, _ := filepath.EvalSymlinks(r.Root)

	l := (&logb{}).
		ev(100, "exec", "-", root, "/bin/sh ./means.sh").
		ev(100, "open", "r", root+"/means.sh", "sha256=sh").
		ev(100, "open", "w", root+"/out.csv", "new=1").
		ev(100, "closew", "w", root+"/out.csv", "sha256="+EmptySHA256).
		ev(101, "exec", "-", root, "awk -F, NR>1{s+=$2;n++} data/pk.csv").
		ev(101, "inherit", "w", root+"/out.csv", "").
		ev(101, "open", "r", root+"/data/pk.csv", "sha256=pk").
		ev(101, "closew", "w", root+"/out.csv", "sha256=out")
	s := NewSession("t", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp})
	s.Line("#ktontrace v2\thash=" + root) // the shim names the project in the header
	for _, line := range l.lines {
		s.Line(line)
	}
	s.Close("eof", l.ts+1)
	tail := task(t, s, "tail")
	if len(tail.Outputs) != 1 || tail.Outputs[0].Path != root+"/out.csv" || tail.Outputs[0].SHA256 != "out" {
		t.Fatalf("the redirect is the script's output, not its console: %+v", tail.Outputs)
	}
	d := MakeDraft(s, tail)
	if d.Cmd != "/bin/sh ./means.sh" {
		t.Fatalf("cmd %q: the command the session ran, not the placeholder", d.Cmd)
	}
	var ins, outs []string
	for _, f := range d.Inputs {
		if f.Use {
			ins = append(ins, f.Path)
		}
	}
	for _, f := range d.Outputs {
		if f.Use {
			outs = append(outs, f.Path)
		}
	}
	b, _ := json.Marshal(map[string]any{"inputs": ins, "outputs": outs, "cmd": d.Cmd})
	if out, code := r.Cockpit(t, "publish", string(b)); code != 0 || !strings.Contains(out, "sha256:") {
		t.Fatalf("cockpit publish %s exited %d:\n%s", b, code, out)
	}
}

// ADR-010: an orchestrator's qualification needs a record per nmfe call. The inner call of a
// PsN vpc is drafted on its own (its files, its command from its directory, the licence as a
// note) and cockpit publish takes it.
func TestCallDraft_OneNmfeCallIsPublishedOnItsOwn(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "models/run6.mod", "$PROB run6\n")
	r.Write(t, "data/pk.csv", "id,dv\n1,2\n")
	r.Write(t, "models/vpc/NM_run1/psn.mod", "$PROB psn\n")
	r.Write(t, "models/vpc/NM_run1/psn.ext", "ITERATION OBJ\n0 104.561\n")
	r.Write(t, "models/vpc/vpc_results.csv", "q,v\n")
	root, _ := filepath.EvalSymlinks(r.Root)
	run1 := root + "/models/vpc/NM_run1"
	task := &Task{ID: "16", Session: "s",
		Parents: map[int]int{21: 20, 22: 21},
		Commands: []Command{
			{PID: 20, PPID: 1, Cmdline: "perl /usr/local/bin/vpc run6.mod -samples=10", Cwd: root + "/models"},
			{PID: 22, PPID: 21, Cmdline: "/bin/bash /opt/nm/run/nmfe75 psn.mod psn.lst", Cwd: run1},
		},
		Ops: []FileOp{
			{PID: 20, Op: "r", Path: root + "/models/run6.mod", SHA256: "m6"},
			{PID: 21, Op: "w", Path: run1 + "/psn.mod"},
			{PID: 21, Op: "c", Path: run1 + "/psn.mod", SHA256: "p"},
			{PID: 22, Op: "r", Path: run1 + "/psn.mod", SHA256: "p"},
			{PID: 22, Op: "r", Path: root + "/data/pk.csv", SHA256: "d"},
			{PID: 22, Op: "r", Path: "/opt/nm/license/nonmem.lic", SHA256: "lic", Outside: true},
			{PID: 22, Op: "w", Path: run1 + "/psn.ext"},
			{PID: 22, Op: "c", Path: run1 + "/psn.ext", SHA256: "e"},
			{PID: 20, Op: "w", Path: root + "/models/vpc/vpc_results.csv"},
			{PID: 20, Op: "c", Path: root + "/models/vpc/vpc_results.csv", SHA256: "v"},
		}}
	cs := CallsOf(task)
	if len(cs) != 2 || cs[1].PID != 22 {
		t.Fatalf("calls %+v", cs)
	}
	d := CallDraft(cs[1], root, nil)
	if d.Cmd != "cd models/vpc/NM_run1 && nmfe75 psn.mod psn.lst" {
		t.Fatalf("cmd %q", d.Cmd)
	}
	var ins, outs []string
	for _, f := range d.Inputs {
		ins = append(ins, f.Path)
	}
	for _, f := range d.Outputs {
		outs = append(outs, f.Path)
	}
	if strings.Join(ins, ",") != "models/vpc/NM_run1/psn.mod,data/pk.csv" || strings.Join(outs, ",") != "models/vpc/NM_run1/psn.ext" {
		t.Fatalf("inputs %v outputs %v", ins, outs)
	}
	notes := strings.Join(d.Notes, "; ")
	if !strings.Contains(notes, "nonmem.lic") || !strings.Contains(notes, "pid 20") {
		t.Fatalf("notes %q: the licence and the enclosing vpc", notes)
	}
	b, _ := json.Marshal(map[string]any{"inputs": ins, "outputs": outs, "cmd": d.Cmd})
	if out, code := r.Cockpit(t, "publish", string(b)); code != 0 || !strings.Contains(out, "sha256:") {
		t.Fatalf("cockpit publish %s exited %d:\n%s", b, code, out)
	}
}

func TestShellJoin_QuotesWhatAShellWouldSplit(t *testing.T) {
	if got := shellJoin([]string{"nmfe75", "run 1.mod", "it's", "-x=a/b"}); got != `nmfe75 'run 1.mod' 'it'\''s' -x=a/b` {
		t.Fatalf("%s", got)
	}
}

// Review R1 of #12: a path inside the repository stays that path, relative to the call's
// directory even above it; only a path outside the repository becomes its file name. Before,
// all three rows lost their directory and the record named files that are not there.
func TestNormArgv_RepoPathsKeepTheirPlaceOnlyOutsidePathsLoseIt(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "models/run1/.keep", "")
	r.Write(t, "analysis/.keep", "")
	root, _ := filepath.EvalSymlinks(r.Root)
	for _, row := range []struct{ cmdline, cwd, want string }{
		{"/opt/nm/run/nmfe75 " + root + "/data/pk.mod run1.lst", root + "/models/run1", "nmfe75 ../../data/pk.mod run1.lst"},
		{"Rscript " + root + "/R/fit.R", root + "/analysis", "Rscript ../R/fit.R"},
		{"/usr/bin/execute run1.mod --data=" + root + "/data/pk.csv", root + "/models", "execute run1.mod --data=../data/pk.csv"},
		{"/opt/nm/run/nmfe75 run1.mod -licfile=/opt/nm/license/nonmem.lic", root + "/models", "nmfe75 run1.mod -licfile=nonmem.lic"},
	} {
		if got := strings.Join(normArgv(row.cmdline, row.cwd), " "); got != row.want {
			t.Errorf("%s (in %s):\n got  %s\n want %s", row.cmdline, row.cwd, got, row.want)
		}
	}
}

// Re-check of #12: a submodule's .git file does not end the repository; the parent's root counts.
func TestRepoRoot_ASubmoduleIsPartOfItsParent(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{".git", "lib/sub/models"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "lib/sub/.git"), []byte("gitdir: ../../.git/modules/sub\n"), 0o644)
	if got := RepoRoot(filepath.Join(root, "lib/sub/models")); got != root {
		t.Fatalf("root %q, want the parent %q", got, root)
	}
	lone := t.TempDir() // a worktree: a .git file and nothing above
	os.MkdirAll(filepath.Join(lone, "a"), 0o755)
	os.WriteFile(filepath.Join(lone, ".git"), []byte("gitdir: /elsewhere\n"), 0o644)
	if got := RepoRoot(filepath.Join(lone, "a")); got != lone {
		t.Fatalf("worktree root %q, want %q", got, lone)
	}
}
