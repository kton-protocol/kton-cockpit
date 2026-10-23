package ktonmcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if strings.Join(names, " ") != "kton_ask kton_copy kton_note kton_open_graph kton_open_story kton_projects kton_runs kton_select kton_selection kton_sparql kton_story" {
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

// fakeEditor is a Positron window: it listens to ktonwatch's events and answers each with do.
func fakeEditor(t *testing.T, sv *watch.Service, do func(watch.UIEvent) watch.UIAnswer) {
	t.Helper()
	srv := httptest.NewServer(sv.Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/api/ui/events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				var ev watch.UIEvent
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
				a := do(ev)
				a.ID = ev.ID
				b, _ := json.Marshal(a)
				http.Post(srv.URL+"/api/ui/answer", "application/json", strings.NewReader(string(b)))
			}
		}
	}()
	for i := 0; sv.UI.Listening() == 0 && i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEditorTools_SayWhenNoWindowListens(t *testing.T) {
	sv, project := fixture(t)
	cs := connect(t, sv)
	e := errText(call(t, cs, "kton_open_story", map[string]any{"path": "derived/fit.csv", "project": project}, nil))
	if !strings.Contains(e, "no Positron window") {
		t.Fatalf("%q", e)
	}
}

func TestEditorTools_TheWindowGetsWhatItNeeds(t *testing.T) {
	sv, project := fixture(t)
	cs := connect(t, sv)
	var got []watch.UIEvent
	fakeEditor(t, sv, func(ev watch.UIEvent) watch.UIAnswer {
		got = append(got, ev)
		switch ev.Action {
		case "get_selection":
			return watch.UIAnswer{OK: true, Message: "1 step selected", Data: map[string]any{"selection": []any{map[string]any{"id": "sha256:abc"}}}}
		case "copy":
			return watch.UIAnswer{OK: false, Message: "declined"}
		}
		return watch.UIAnswer{OK: true, Message: "opened"}
	})
	var out EditorOut
	call(t, cs, "kton_open_story", map[string]any{"path": "derived/fit.csv", "project": project}, &out)
	if !out.OK || got[0].Action != "open_story" || got[0].Args["path"] != filepath.Join(project, "derived/fit.csv") || len(got[0].Args["sha256"].(string)) != 64 {
		t.Fatalf("open_story: %+v %+v", out, got[0])
	}
	call(t, cs, "kton_select", map[string]any{"project": project, "items": []string{"derived/pk.csv", "sha256:" + strings.Repeat("a", 64)}}, &out)
	if sel := got[1].Args["selection"].([]any); len(sel) != 2 || !strings.HasPrefix(sel[0].(string), "sha256:") || len(sel[0].(string)) != 71 {
		t.Fatalf("select: %+v", got[1])
	}
	call(t, cs, "kton_selection", map[string]any{}, &out)
	if out.Data["selection"] == nil {
		t.Fatalf("selection: %+v", out)
	}
	call(t, cs, "kton_copy", map[string]any{"hash": "sha256:" + strings.Repeat("b", 64), "name": "R/04_fit_population.R", "project": project}, &out)
	if out.OK || out.Message != "declined" || got[3].Args["to"] != "R/04_fit_population.R" {
		t.Fatalf("copy: %+v %+v", out, got[3])
	}
	if e := errText(call(t, cs, "kton_copy", map[string]any{"hash": "sha256:" + strings.Repeat("b", 64), "to": "../outside.R", "project": project}, nil)); !strings.Contains(e, "inside the project") {
		t.Fatalf("a target outside the project: %q", e)
	}
}

func TestSparql_PassesTheQueryAndShowsTheAnswerOrTheRefusal(t *testing.T) {
	sv, project := fixture(t)
	cs := connect(t, sv)
	var sent []byte
	askJSON = func(_ context.Context, dir string, q any) ([]byte, error) {
		sent, _ = json.Marshal(q)
		if strings.Contains(string(sent), "SERVICE") {
			return nil, errors.New("exit status 2: query.refused: SERVICE calls another endpoint")
		}
		return []byte(`{"query":"sparql","sparql":{"form":"SELECT","vars":["step"],"rows":[{"step":"https://kton.dev/o/ab"}],"limit":5}}`), nil
	}
	t.Cleanup(func() { askJSON = watch.CockpitAskJSON })
	var out AskOut
	call(t, cs, "kton_sparql", map[string]any{"sparql": "SELECT ?step WHERE { ?step a <http://www.w3.org/ns/prov#Activity> }", "project": project, "limit": 5}, &out)
	if !strings.Contains(string(sent), `"limit":5`) || !strings.Contains(string(sent), `"query":"sparql"`) {
		t.Fatalf("sent %s", sent)
	}
	if a, _ := out.Answer.(map[string]any); a == nil || a["form"] != "SELECT" {
		t.Fatalf("answer %+v", out.Answer)
	}
	if e := errText(call(t, cs, "kton_sparql", map[string]any{"sparql": "SELECT * { SERVICE <x> { ?s ?p ?o } }", "project": project}, nil)); !strings.Contains(e, "query.refused") {
		t.Fatalf("refusal: %q", e)
	}
	if e := errText(call(t, cs, "kton_sparql", map[string]any{"sparql": "SELECT 1"}, nil)); !strings.Contains(e, "project") {
		t.Fatalf("no project: %q", e)
	}
}

// The story tab once kept a line's note under session/<where>; the tools keep it per step
// (session/task). Each side finds the other's.
func TestNotes_AnOldLineKeyIsFoundForItsStep(t *testing.T) {
	sv, project := fixture(t)
	sv.Notes.Put("positron-1/R/01_prepare.R", watch.Note{Description: "written in the story before", By: "you"})
	cs := connect(t, sv)
	var runs RunsOut
	call(t, cs, "kton_runs", map[string]any{"project": project}, &runs)
	if n := runs.Runs[0].Steps[0].Note; n == nil || n.Description != "written in the story before" {
		t.Fatalf("runs: %+v", runs.Runs[0].Steps[0])
	}
	var st StoryOut
	call(t, cs, "kton_story", map[string]any{"path": "derived/fit.csv", "project": project}, &st)
	if n := st.Steps[0].Note; n == nil || n.Description != "written in the story before" {
		t.Fatalf("story: %+v", st.Steps[0])
	}
}

func TestTour_APromptWithItsExploreRepository(t *testing.T) {
	sv, _ := fixture(t)
	cs := connect(t, sv)
	ps, err := cs.ListPrompts(context.Background(), nil)
	if err != nil || len(ps.Prompts) != 1 || ps.Prompts[0].Name != "pmx_tour" {
		t.Fatalf("prompts %+v %v", ps, err)
	}
	r, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "pmx_tour", Arguments: map[string]string{"explore": "/x/explore"}})
	if err != nil {
		t.Fatal(err)
	}
	text := r.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, `project = "/x/explore"`) || strings.Contains(text, "EXPLORE") || !strings.Contains(text, "PREFIX pmx:") {
		t.Fatalf("tour text: %.300s", text)
	}
}

// kton_open_graph on a repository that is no recorded project (an explore repository): the MCP
// admits it to /kton/reg/<id>, and the editor is asked to open it by that id.
func TestEditorTools_AGraphOfAnExploreRepository(t *testing.T) {
	sv, _ := fixture(t)
	cs := connect(t, sv)
	explore := t.TempDir()
	os.WriteFile(filepath.Join(explore, "cockpit.config.json"), []byte("{}"), 0o644)
	var got []watch.UIEvent
	fakeEditor(t, sv, func(ev watch.UIEvent) watch.UIAnswer {
		got = append(got, ev)
		return watch.UIAnswer{OK: true, Message: "the graph shows it"}
	})
	if sv.RegistryRepo(watch.RegistryID(explore)) != "" {
		t.Fatal("admitted before it was asked for")
	}
	var out EditorOut
	call(t, cs, "kton_open_graph", map[string]any{"repo": explore, "hash": "sha256:" + strings.Repeat("c", 64), "title": "PMX-02"}, &out)
	if !out.OK || got[0].Args["registry"] != watch.RegistryID(explore) || got[0].Args["title"] != "PMX-02" {
		t.Fatalf("%+v %+v", out, got)
	}
	if sv.RegistryRepo(watch.RegistryID(explore)) != explore {
		t.Fatal("not admitted to /kton/reg")
	}
	if e := errText(call(t, cs, "kton_open_graph", map[string]any{"repo": t.TempDir(), "hash": "sha256:" + strings.Repeat("c", 64)}, nil)); !strings.Contains(e, "not a cockpit repository") {
		t.Fatalf("a plain directory: %q", e)
	}
}

// "~/kton-programme/explore", as a person (and the tour) writes it: MH's searches found nothing
// because the cockpit was asked in a directory literally named "~".
func TestPaths_TheHomeDirectoryIsExpanded(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	explore := filepath.Join(h, "kton-programme", "explore")
	os.MkdirAll(explore, 0o755)
	os.WriteFile(filepath.Join(explore, "cockpit.config.json"), []byte("{}"), 0o644)
	if got := home("~/kton-programme/explore/"); got != explore {
		t.Fatalf("home: %q", got)
	}
	if home("sha256:abc") != "sha256:abc" || home("derived/fit.csv") != "derived/fit.csv" || home("") != "" {
		t.Fatal("home changed what is not a home path")
	}
	sv, _ := fixture(t)
	cs := connect(t, sv)
	var dir string
	askJSON = func(_ context.Context, d string, q any) ([]byte, error) {
		dir = d
		return []byte(`{"sparql":{"form":"SELECT","vars":[],"rows":[]}}`), nil
	}
	t.Cleanup(func() { askJSON = watch.CockpitAskJSON })
	call(t, cs, "kton_sparql", map[string]any{"sparql": "SELECT * {}", "project": "~/kton-programme/explore"}, nil)
	if dir != explore {
		t.Fatalf("asked in %q, want %q", dir, explore)
	}
}
