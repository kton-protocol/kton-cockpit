package ktonmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A project with one closed R session of two steps, as ktonwatch keeps it after a restart:
// 01_prepare.R reads the data and writes pk.csv, then a console line writes fit.csv from it.
func fixture(t *testing.T) (*watch.Service, string) {
	t.Helper()
	project, store := t.TempDir(), t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(project, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
		h := sha256.Sum256([]byte(body))
		return hex.EncodeToString(h[:])
	}
	data, script, pk, fit := write("data/pk.csv", "Subject,conc\n1,2\n"), write("R/01_prepare.R", "pk <- read.csv(...)\n"),
		write("derived/pk.csv", "pk\n"), write("derived/fit.csv", "fit\n")
	in := func(rel, sum string) *watch.Input {
		return &watch.Input{Path: filepath.Join(project, rel), SHA256: sum, Class: watch.ClassProject, Status: "observed"}
	}
	out := func(rel, sum string) *watch.Output {
		return &watch.Output{Path: filepath.Join(project, rel), SHA256: sum, Class: watch.ClassProject}
	}
	from := func(i *watch.Input, task string) *watch.Input { // as the session links a read to the step that wrote it
		i.From = []watch.Ref{{Task: task, Path: i.Path}}
		return i
	}
	s := watch.Session{ID: "positron-1", Log: filepath.Join(store, "gone.tsv"), Project: project, Start: 1e18, Last: 1e18 + 9e9, Closed: true,
		Tasks: []*watch.Task{
			{ID: "1", Seq: 1, Status: "ok", Start: 1e18, End: 1e18 + 3e9,
				Origins: []watch.Origin{{Kind: "script", File: filepath.Join(project, "R/01_prepare.R"), Code: `source("R/01_prepare.R")`, TS: 1e18 + 1e9, Language: "r"}},
				Sources: []watch.Source{{Path: filepath.Join(project, "R/01_prepare.R"), Class: watch.ClassProject}},
				Inputs:  []*watch.Input{in("R/01_prepare.R", script), in("data/pk.csv", data)}, Outputs: []*watch.Output{out("derived/pk.csv", pk)}},
			{ID: "2", Seq: 2, Status: "ok", Start: 1e18 + 3e9, End: 1e18 + 9e9, Expr: `write.csv(fit(read.csv("derived/pk.csv")), "derived/fit.csv")`,
				Inputs: []*watch.Input{from(in("derived/pk.csv", pk), "1")}, Outputs: []*watch.Output{out("derived/fit.csv", fit)}},
		}}
	b, _ := json.Marshal(s)
	os.WriteFile(filepath.Join(store, "positron-1.json"), b, 0o644)
	sv := &watch.Service{Dir: t.TempDir(), Store: store, NewClassifier: func() *watch.Classifier { return &watch.Classifier{} }}
	if err := sv.Load(); err != nil {
		t.Fatal(err)
	}
	return sv, project
}

func connect(t *testing.T, sv *watch.Service) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := Server(sv, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, into any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if into != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, into); err != nil {
			t.Fatalf("%s: %v in %s", tool, err, b)
		}
	}
	return res
}

func errText(res *mcp.CallToolResult) string {
	if !res.IsError || len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func TestTools_Listed(t *testing.T) {
	sv, _ := fixture(t)
	cs := connect(t, sv)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	if strings.Join(names, " ") != "kton_ask kton_note kton_projects kton_runs kton_story" {
		t.Fatalf("tools %v", names)
	}
}

func TestTools_ProjectsRunsAndANoteRoundTrip(t *testing.T) {
	sv, project := fixture(t)
	cs := connect(t, sv)
	var ps ProjectsOut
	call(t, cs, "kton_projects", nil, &ps)
	if len(ps.Projects) != 1 || ps.Projects[0].Path != project {
		t.Fatalf("projects %+v", ps)
	}
	var runs RunsOut
	call(t, cs, "kton_runs", map[string]any{"project": project}, &runs)
	if len(runs.Runs) != 1 || len(runs.Runs[0].Steps) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	st := runs.Runs[0].Steps[0]
	if st.Key != "positron-1/1" || st.Where != "R/01_prepare.R" || strings.Join(st.Writes, ",") != "derived/pk.csv" || st.Note != nil {
		t.Fatalf("step %+v", st)
	}

	if e := errText(call(t, cs, "kton_note", map[string]any{"key": "positron-1/9", "description": "x"}, nil)); !strings.Contains(e, "no recorded step") {
		t.Fatalf("a note on a step that does not exist: %q", e)
	}
	var n NoteOut
	call(t, cs, "kton_note", map[string]any{"key": "positron-1/1", "description": "Prepares the PK data.", "rationale": "Dose in mg for the population model."}, &n)
	if n.Note.By != "claude" || n.Note.Description != "Prepares the PK data." || n.Was != nil {
		t.Fatalf("note %+v", n)
	}
	call(t, cs, "kton_runs", map[string]any{"project": project}, &runs)
	if got := runs.Runs[0].Steps[0].Note; got == nil || got.Rationale != "Dose in mg for the population model." {
		t.Fatalf("the draft is not in the runs: %+v", got)
	}
	// the story tab reads the same store
	if all := sv.Notes.All("positron-1/"); all["positron-1/1"].Description != "Prepares the PK data." {
		t.Fatalf("store %+v", all)
	}
	// and it survives a restart
	if again := watch.OpenNotes(filepath.Join(sv.Store, "notes")); again.All()["positron-1/1"].By != "claude" {
		t.Fatalf("not kept: %+v", again.All())
	}
}

func TestTools_TheStoryOfAFile(t *testing.T) {
	sv, project := fixture(t)
	cs := connect(t, sv)
	var st StoryOut
	res := call(t, cs, "kton_story", map[string]any{"path": filepath.Join(project, "derived/fit.csv")}, &st)
	if res.IsError {
		t.Fatal(errText(res))
	}
	if st.Project != project || st.File != "derived/fit.csv" || len(st.Steps) != 2 || !st.Steps[0].OnPath || !st.Steps[1].OnPath {
		t.Fatalf("story %+v", st)
	}
	if st.Steps[0].Where != "R/01_prepare.R" || st.Steps[1].Where != "console" {
		t.Fatalf("where %q, %q", st.Steps[0].Where, st.Steps[1].Where)
	}
	// relative paths need the project; a file nobody wrote has no story
	if e := errText(call(t, cs, "kton_story", map[string]any{"path": "derived/fit.csv"}, nil)); !strings.Contains(e, "relative") {
		t.Fatalf("relative without project: %q", e)
	}
	if e := errText(call(t, cs, "kton_story", map[string]any{"path": "data/pk.csv", "project": project}, nil)); !strings.Contains(e, "no recorded step wrote") {
		t.Fatalf("a source has no story: %q", e)
	}
}

func TestHandler_NeedsTheToken(t *testing.T) {
	sv, _ := fixture(t)
	tok, err := Token(filepath.Join(t.TempDir(), "kton", "mcp-token"))
	if err != nil || len(tok) != 64 {
		t.Fatalf("token %q %v", tok, err)
	}
	srv := httptest.NewServer(Handler(Server(sv, "test"), tok))
	defer srv.Close()
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	post := func(auth string) int {
		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(init))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := post(""); c != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", c)
	}
	if c := post("Bearer " + strings.Repeat("0", 64)); c != http.StatusUnauthorized {
		t.Fatalf("with a wrong token: %d", c)
	}
	if c := post("Bearer " + tok); c != http.StatusOK {
		t.Fatalf("with the token: %d", c)
	}
}

func TestToken_MadeOnceReadableByTheOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kton", "mcp-token")
	a, _ := Token(p)
	b, _ := Token(p)
	fi, err := os.Stat(p)
	if err != nil || a != b || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%q %q %v %v", a, b, err, fi.Mode())
	}
}
