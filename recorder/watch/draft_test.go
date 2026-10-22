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
		} else if !strings.Contains(f.Note, "nicht im Repo") {
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
