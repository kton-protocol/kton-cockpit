package watch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// a cockpit repository (ADR-016 layout) with a few files
func editsRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".kton"), 0o755)
	os.WriteFile(filepath.Join(repo, ".kton", "cockpit.config.json"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(repo, "data"), 0o755)
	os.WriteFile(filepath.Join(repo, "data", "pk.csv"), []byte("id,conc\n1,2\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "data", "derived.csv"), []byte("id,c\n1,2\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "report.html"), []byte("<p>report</p>"), 0o644)
	return repo
}

func TestApplyEdit_ChoosesExactlyTheListedFiles(t *testing.T) {
	d := Draft{
		Inputs:  []DraftFile{{Path: "a.R", Use: true}, {Path: "b.csv", Use: true}, {Path: "lib.R", Use: false}},
		Outputs: []DraftFile{{Path: "out.png", Use: true}},
		Cmd:     "Rscript a.R",
	}
	ins, cmd := []string{"lib.R", "a.R", "extra.csv"}, "Rscript a.R --fast"
	got := ApplyEdit(d, StepEdit{Inputs: &ins, Cmd: &cmd})
	if !got.Edited || got.Cmd != cmd {
		t.Fatalf("edited/cmd: %+v", got)
	}
	want := []DraftFile{{Path: "lib.R", Use: true}, {Path: "a.R", Use: true}, {Path: "extra.csv", Use: true, Note: "added by hand: the recording did not see it"}, {Path: "b.csv", Use: false}}
	if len(got.Inputs) != len(want) {
		t.Fatalf("inputs %+v", got.Inputs)
	}
	for i := range want {
		if got.Inputs[i] != want[i] {
			t.Fatalf("input %d: got %+v, want %+v", i, got.Inputs[i], want[i])
		}
	}
	if len(got.Outputs) != 1 || !got.Outputs[0].Use {
		t.Fatalf("outputs not to be touched: %+v", got.Outputs)
	}
	if ApplyEdit(d, StepEdit{}).Edited {
		t.Fatal("an empty edit is no edit")
	}
	// kton-review D1: what the person added (a file never seen, another cmd) makes the step declared=hand;
	// leaving out or taking in files the recorder saw stays an observation
	if got.Declared != "hand" {
		t.Fatalf("an added file and a changed cmd: declared %q", got.Declared)
	}
	narrowed := []string{"a.R"}
	if n := ApplyEdit(d, StepEdit{Inputs: &narrowed}); !n.Edited || n.Declared != "" {
		t.Fatalf("only left out recorded files: %+v", n)
	}
	seen := []string{"a.R", "lib.R"} // lib.R was seen, unticked by the recorder
	if n := ApplyEdit(d, StepEdit{Inputs: &seen}); n.Declared != "" {
		t.Fatalf("took in a file the recorder saw: %+v", n)
	}
	same := "Rscript a.R"
	if n := ApplyEdit(d, StepEdit{Cmd: &same}); n.Declared != "" {
		t.Fatalf("the same cmd is no change: %+v", n)
	}
	other := "Rscript a.R --fast"
	if n := ApplyEdit(d, StepEdit{Cmd: &other}); n.Declared != "hand" {
		t.Fatalf("a changed cmd: %+v", n)
	}
}

func TestEdits_PutStepRefusesPathsOutsideAndBadKeys(t *testing.T) {
	e := OpenEdits(t.TempDir())
	for _, p := range []string{"../x", "/etc/passwd", "a/../b", "a//b", `a\b`, ""} {
		l := []string{p}
		if err := e.PutStep("s1/3", StepEdit{Inputs: &l}); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	l := []string{"a.R"}
	for _, k := range []string{"declared/x", "nokey"} {
		if err := e.PutStep(k, StepEdit{Inputs: &l}); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
	if err := e.PutStep("s1/3", StepEdit{Inputs: &l}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Step("s1/3"); !ok {
		t.Fatal("not stored")
	}
	// an edit that sets nothing goes back to the recording
	if err := e.PutStep("s1/3", StepEdit{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Step("s1/3"); ok {
		t.Fatal("an empty edit should remove it")
	}
}

func TestEdits_DeclareHashesNowAndTheDraftSeesChanges(t *testing.T) {
	repo := editsRepo(t)
	dir := t.TempDir()
	e := OpenEdits(dir)
	bad := []DeclareRequest{
		{Project: "rel/path", Outputs: []string{"report.html"}, Cmd: "x"},
		{Project: repo, Cmd: "x"},                                                        // no output
		{Project: repo, Outputs: []string{"report.html"}},                                // no cmd
		{Project: repo, Outputs: []string{"../outside"}, Cmd: "x"},                       // leaves the repo
		{Project: repo, Outputs: []string{"nope.txt"}, Cmd: "x"},                         // not there
		{Project: repo, Outputs: []string{"data"}, Cmd: "x"},                             // a folder
		{Project: repo, Declared: "machine", Outputs: []string{"report.html"}, Cmd: "x"}, // not hand|heuristic:<name>
		{Project: repo, Inputs: []string{"report.html"}, Outputs: []string{"report.html"}, Cmd: "x"},
	}
	for i, in := range bad {
		if _, err := e.Declare(in); err == nil {
			t.Errorf("bad request %d accepted: %+v", i, in)
		}
	}
	// kton-review S1: a link in the repository to a file outside it is not declared as the repository's
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("not the repo's"), 0o644)
	if err := os.Symlink(outside, filepath.Join(repo, "link.txt")); err == nil {
		if _, err := e.Declare(DeclareRequest{Project: repo, Outputs: []string{"link.txt"}, Cmd: "x"}); err == nil || !strings.Contains(err.Error(), "outside the repository") {
			t.Fatalf("a link out of the repository was declared: %v", err)
		}
		os.Symlink("data/pk.csv", filepath.Join(repo, "inside-link.csv")) // a link within the repository is fine
		if _, err := e.Declare(DeclareRequest{Project: repo, Outputs: []string{"inside-link.csv"}, Cmd: "x"}); err != nil {
			t.Fatalf("a link inside the repository: %v", err)
		}
		e.data.Declared = nil
	}
	d, err := e.Declare(DeclareRequest{Project: repo, Inputs: []string{"data/pk.csv"}, Outputs: []string{"data/derived.csv"}, Cmd: "derived.csv from pk.csv in Excel"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Declared != "hand" || d.ID == "" || d.By != "you" || len(d.Inputs[0].SHA256) != 64 {
		t.Fatalf("declared %+v", d)
	}
	dr := DeclaredDraft(d)
	if dr.Declared != "hand" || dr.Repo != repo || !dr.Inputs[0].Use || !dr.Outputs[0].Use || dr.Cmd != d.Cmd {
		t.Fatalf("draft %+v", dr)
	}
	// the output changed since it was picked: not signed with other bytes
	os.WriteFile(filepath.Join(repo, "data", "derived.csv"), []byte("other"), 0o644)
	if dr := DeclaredDraft(d); dr.Outputs[0].Use || !strings.Contains(dr.Outputs[0].Note, "changed since it was picked") {
		t.Fatalf("changed output still used: %+v", dr.Outputs)
	}
	// a second store over the same folder reads it back; replacing keeps the id
	e2 := OpenEdits(dir)
	if got := e2.Declared(repo); len(got) != 1 || got[0].ID != d.ID {
		t.Fatalf("reread %+v", got)
	}
	r, err := e2.Declare(DeclareRequest{ID: d.ID, Project: repo, Declared: "heuristic:nonmem-dir", Outputs: []string{"data/derived.csv"}, Cmd: "again"})
	if err != nil || r.ID != d.ID || r.Declared != "heuristic:nonmem-dir" || len(e2.Declared("")) != 1 {
		t.Fatalf("replace: %+v %v", r, err)
	}
	if err := e2.Undeclare(d.ID); err != nil || len(e2.Declared("")) != 0 {
		t.Fatalf("undeclare: %v", err)
	}
}

func TestWithDeclared_StoryOfAFileOnlyADeclaredStepMade(t *testing.T) {
	repo := editsRepo(t)
	e := OpenEdits(t.TempDir())
	prep, _ := e.Declare(DeclareRequest{Project: repo, Inputs: []string{"data/pk.csv"}, Outputs: []string{"data/derived.csv"}, Cmd: "cleaned in Excel"})
	rep, _ := e.Declare(DeclareRequest{Project: repo, Inputs: []string{"data/derived.csv"}, Outputs: []string{"report.html"}, Cmd: "wrote the report"})
	// nothing recorded: the story of report.html is the two declared steps, the one that made its input first
	st := withDeclared(nil, repo, filepath.Join(repo, "report.html"), "", e.Declared(repo))
	if st == nil || len(st.Steps) != 2 {
		t.Fatalf("story %+v", st)
	}
	if st.Steps[0].Task != prep.ID || st.Steps[1].Task != rep.ID {
		t.Fatalf("order: %s %s", st.Steps[0].Task, st.Steps[1].Task)
	}
	s := st.Steps[1]
	if s.Session != DeclaredSession || s.Declared != "hand" || s.Where != "declared by hand" || !s.OnPath || s.Writes[0] != "report.html" {
		t.Fatalf("step %+v", s)
	}
	// another file's bytes: no story from these steps
	if withDeclared(nil, repo, filepath.Join(repo, "report.html"), "sha256:"+strings.Repeat("0", 64), e.Declared(repo)) != nil {
		t.Fatal("a story for bytes no declared step made")
	}
	// a recorded story that reads derived.csv gets the declared step before its first reader
	rec := &Story{Target: FileVersion{Path: filepath.Join(repo, "out.png"), When: 500}, Steps: []StoryStep{{Session: "s1", Task: "4", When: 300, Reads: []string{"data/derived.csv"}, Writes: []string{"out.png"}}}}
	st = withDeclared(rec, repo, filepath.Join(repo, "out.png"), "", e.Declared(repo))
	if len(st.Steps) != 2 || st.Steps[0].Task != prep.ID || st.Steps[0].When >= 300 {
		t.Fatalf("recorded story with the declared step: %+v", st.Steps)
	}
}

func TestAPI_EditsAndDeclaredSteps(t *testing.T) {
	repo := editsRepo(t)
	store := t.TempDir()
	sv := &Service{Dir: t.TempDir(), Store: store, Idle: time.Hour, NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Watched = OpenWatched(store, filepath.Join(t.TempDir(), "hashes"))
	sv.Edits = OpenEdits(filepath.Join(store, "edits"))
	sv.Poll(time.Now())
	if err := sv.Watched.SetFolder(repo, true); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(sv.Handler())
	defer srv.Close()
	send := func(method, path string, body any) (*http.Response, []byte) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		r, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out bytes.Buffer
		out.ReadFrom(r.Body)
		return r, out.Bytes()
	}
	// a folder the recorder does not record takes no declared step
	if r, _ := send("POST", "/api/declared", DeclareRequest{Project: t.TempDir(), Outputs: []string{"x"}, Cmd: "x"}); r.StatusCode != 400 {
		t.Fatalf("unwatched folder: %d", r.StatusCode)
	}
	r, body := send("POST", "/api/declared", DeclareRequest{Project: repo, Inputs: []string{"data/pk.csv"}, Outputs: []string{"report.html"}, Cmd: "wrote it by hand"})
	if r.StatusCode != 200 {
		t.Fatalf("declare: %d %s", r.StatusCode, body)
	}
	var d DeclaredStep
	json.Unmarshal(body, &d)
	r, body = send("GET", "/api/draft?session=declared&task="+url.QueryEscape(d.ID), nil)
	var dr Draft
	json.Unmarshal(body, &dr)
	if r.StatusCode != 200 || dr.Declared != "hand" || len(dr.Outputs) != 1 || dr.Outputs[0].Path != "report.html" {
		t.Fatalf("declared draft: %d %s", r.StatusCode, body)
	}
	r, body = send("GET", "/api/story?project="+url.QueryEscape(repo)+"&path="+url.QueryEscape(filepath.Join(repo, "report.html")), nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"declared":"hand"`) {
		t.Fatalf("story: %d %s", r.StatusCode, body)
	}
	// a recorded step's edit is stored and listed; an empty one goes
	ins := []string{"data/pk.csv"}
	if r, body := send("PUT", "/api/edits", map[string]any{"key": "s1/3", "inputs": ins}); r.StatusCode != 200 {
		t.Fatalf("put edit: %d %s", r.StatusCode, body)
	}
	r, body = send("GET", "/api/edits?project="+url.QueryEscape(repo), nil)
	var all struct {
		Steps    map[string]StepEdit `json:"steps"`
		Declared []DeclaredStep      `json:"declared"`
	}
	json.Unmarshal(body, &all)
	if e, ok := all.Steps["s1/3"]; !ok || e.By != "you" || len(all.Declared) != 1 {
		t.Fatalf("edits: %s", body)
	}
	if r, _ := send("PUT", "/api/edits", map[string]any{"key": "s1/3"}); r.StatusCode != 200 {
		t.Fatal("clear edit")
	}
	if r, _ := send("DELETE", "/api/declared?id="+url.QueryEscape(d.ID), nil); r.StatusCode != 200 {
		t.Fatal("undeclare")
	}
	if r, _ := send("GET", "/api/draft?session=declared&task="+url.QueryEscape(d.ID), nil); r.StatusCode != 404 {
		t.Fatalf("draft of a removed declared step: %d", r.StatusCode)
	}
}
