// Package ktonmcp lets an assistant work with what the recorder saw, over MCP: browse the projects,
// the recorded runs and a file's story, ask kton about a file, and draft a description and a
// rationale for a step. It runs inside `ktonwatch serve` at /mcp (streamable HTTP), next to the
// observations, for the Claude extension in Positron (`claude mcp add --transport http …`).
//
// It signs nothing. A draft is a note the person sees in the story and signs when they publish
// (`cockpit say describes|rationale`), as MH decided: drafts by Claude, signed by the person.
package ktonmcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/mcpsurface"
	"github.com/kton-protocol/kton-cockpit/recorder/watch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is the MCP server over one ktonwatch service.
func Server(sv *watch.Service, version string) *mcp.Server {
	t := &tools{sv: sv}
	s := mcp.NewServer(&mcp.Implementation{Name: "kton-recorder", Version: version}, &mcp.ServerOptions{
		Instructions: "What the kton recorder saw in R, Python and terminal sessions: projects, runs, a file's story " +
			"(how it came about, step by step), and what kton holds about a file. Step keys are \"session/task\". " +
			"kton_note writes a DRAFT description and rationale for a step; the person reviews and signs it in the " +
			"story when publishing. Nothing here signs or publishes. kton_open_story, kton_open_graph and kton_select " +
			"show the person what you mean in their Positron; kton_selection reads what they selected; kton_copy " +
			"copies a file over only after they agree.",
	})
	mcp.AddTool(s, &mcp.Tool{Name: "kton_projects", Description: "The projects the recorder has sessions for, most recent first, with their cockpit repository (registry) if they are in one."},
		mcpsurface.Handler(t.projects))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_runs", Description: "The recorded sessions of a project (R and Python consoles, terminals) with their steps: key, time, where (script, chunk, console), code, files read and written, and the draft note if there is one. Steps and sessions without effect are left out."},
		mcpsurface.Handler(t.runs))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_story", Description: "How a file came about: every step from the start of its sessions up to the file, on_path marking those it was made from, whether kton already has each step (kton: the same computation is published; same: the same output bytes are published but from other inputs; empty: not in kton), the draft notes, and where its source data resides (study › project › programme)."},
		mcpsurface.Handler(t.story))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_ask", Description: "Ask kton (the cockpit, through the project's repository and its federation sources, verified against its trust tiers) about a file or a record: producer (who made these bytes), uses (who used them), lineage (the chain behind them), about (the claims: descriptions, rationales), record (what a record says), reproductions."},
		mcpsurface.Handler(t.ask))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_open_story", Description: "Open a file's story in the person's Positron, as an editor tab: how it came about step by step, with the drafts. Use it to show what you are talking about."},
		mcpsurface.Handler(t.openStory))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_open_graph", Description: "Open kton's graph in the person's Positron, beside the story, centred on a file (its producing step and ancestors): what was recorded and what is signed, joined by file hash, including other analysts' work on the same data. A graph already open for the project is re-centred."},
		mcpsurface.Handler(t.openGraph))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_select", Description: "Select steps in the open graph (opening it if needed): each entry a file (its producing step), a file hash, or a record id (sha256:…). Replaces the selection; an empty list clears it. Use it to point at the steps you mean."},
		mcpsurface.Handler(t.selectSteps))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_selection", Description: "The steps the person has selected in the open graph (Ctrl/Shift-click): record id, command, outputs. Use it for \"these steps\" in what the person asks."},
		mcpsurface.Handler(t.selection))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_copy", Description: "Copy a file (by its content hash, e.g. another analyst's script or dataset found with kton_ask or the graph) into a project. The person is asked in Positron and sees source and target first; nothing is overwritten without their yes. The bytes are checked against the hash."},
		mcpsurface.Handler(t.copyFile))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_sparql", Description: sparqlHelp}, mcpsurface.Handler(t.sparql))
	addTour(s)
	mcp.AddTool(s, &mcp.Tool{Name: "kton_note", Description: "Draft the description and the rationale of a recorded step (key \"session/task\" from kton_runs or kton_story). A draft only: the person sees it in the story, can change it, and signs it when publishing. Empty description and rationale remove the draft."},
		mcpsurface.Handler(t.note))
	return s
}

// Handler serves the MCP server over streamable HTTP, for requests that carry the token.
func Handler(s *mcp.Server, token string) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// a loopback port is open to every process and every web page on the machine
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "kton MCP: a bearer token is needed (kton: Connect Claude, or ~/.config/kton/mcp-token)", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Token is the bearer token in path, made (random, readable by the owner only) when there is none.
func Token(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.ReadFull(f, buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return tok, os.WriteFile(path, []byte(tok+"\n"), 0o600)
}

type tools struct{ sv *watch.Service }

// ------------------------------------------------------------------ projects

type ProjectsIn struct{}
type ProjectsOut struct {
	Projects []watch.ProjectSummary `json:"projects"`
}

func (t *tools) projects(context.Context, ProjectsIn) (*ProjectsOut, error) {
	return &ProjectsOut{Projects: t.sv.Projects()}, nil
}

// ------------------------------------------------------------------ runs

type RunsIn struct {
	Project string `json:"project" jsonschema:"the project directory, as kton_projects names it"`
	Session string `json:"session,omitempty" jsonschema:"only this session"`
}
type Step struct {
	Key    string      `json:"key"`
	When   string      `json:"when"`
	Where  string      `json:"where"`
	Code   string      `json:"code,omitempty"`
	Reads  []string    `json:"reads,omitempty"`
	Writes []string    `json:"writes,omitempty"`
	Error  bool        `json:"error,omitempty"`
	Note   *watch.Note `json:"note,omitempty"`
}
type Run struct {
	Session string `json:"session"`
	Kind    string `json:"kind"` // R console, Python console, terminal
	Start   string `json:"start"`
	Last    string `json:"last"`
	Closed  bool   `json:"closed"`
	Steps   []Step `json:"steps"`
}
type RunsOut struct {
	Project string `json:"project"`
	Runs    []Run  `json:"runs"`
}

func (t *tools) runs(_ context.Context, in RunsIn) (*RunsOut, error) {
	if in.Project == "" {
		return nil, fmt.Errorf("project is needed (kton_projects lists them)")
	}
	out := &RunsOut{Project: in.Project}
	rel := relTo(in.Project)
	for _, s := range t.sv.Sessions() {
		if s.Project != in.Project || (in.Session != "" && s.ID != in.Session) {
			continue
		}
		run := Run{Session: s.ID, Kind: "R console", Start: stamp(s.Start), Last: stamp(s.Last), Closed: s.Closed, Steps: []Step{}}
		if strings.HasPrefix(s.ID, "terminal") || strings.HasPrefix(s.ID, "shell") {
			run.Kind = "terminal"
		}
		t.sv.With(s.ID, func(sess *watch.Session) {
			for _, tk := range sess.Tasks {
				if o := tk.Origin(); o != nil && o.Language == "python" {
					run.Kind = "Python console" // logged like a terminal; its commands say what it is
				}
				if !watch.Effect(tk) && len(tk.Vars) == 0 && tk.Status != "err" {
					continue
				}
				st := Step{Key: sess.ID + "/" + tk.ID, When: stamp(tk.End), Where: watch.StepWhere(tk, rel), Code: clip(tk.Code(), 600), Error: tk.Status == "err"}
				for _, i := range tk.Inputs {
					if i.Class == watch.ClassProject || i.Class == watch.ClassOutside {
						st.Reads = append(st.Reads, rel(i.Path))
					}
				}
				for _, o := range tk.Outputs {
					if o.Class == watch.ClassProject || o.Class == watch.ClassOutside {
						st.Writes = append(st.Writes, rel(o.Path))
					}
				}
				st.Note = t.stepNote(sess.ID, tk.ID, st.Where)
				run.Steps = append(run.Steps, st)
			}
		})
		if len(run.Steps) > 0 || in.Session != "" { // sessions that did nothing with files or variables are noise
			out.Runs = append(out.Runs, run)
		}
	}
	sort.Slice(out.Runs, func(i, j int) bool { return out.Runs[i].Start < out.Runs[j].Start })
	if len(out.Runs) == 0 {
		return nil, fmt.Errorf("no recorded sessions for %s", in.Project)
	}
	return out, nil
}

// ------------------------------------------------------------------ story

type StoryIn struct {
	Path    string `json:"path" jsonschema:"the file, absolute or relative to the project"`
	Project string `json:"project,omitempty" jsonschema:"the project; found from the path when left out"`
	SHA256  string `json:"sha256,omitempty" jsonschema:"the version of the file; the bytes on disk today when left out"`
}
type StoryStep struct {
	Key      string      `json:"key"`
	When     string      `json:"when"`
	Where    string      `json:"where"`
	Sentence string      `json:"sentence"`
	Code     string      `json:"code,omitempty"`
	OnPath   bool        `json:"on_path"`
	InKton   string      `json:"in_kton,omitempty"`
	Hand     bool        `json:"hand,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	Note     *watch.Note `json:"note,omitempty"`
}
type StoryOut struct {
	File    string            `json:"file"`
	SHA256  string            `json:"sha256"`
	Project string            `json:"project"`
	Repo    string            `json:"repo"`
	Verdict string            `json:"verdict"`
	Gaps    []string          `json:"gaps,omitempty"`
	Steps   []StoryStep       `json:"steps"`
	Context []watch.Placement `json:"context,omitempty"`
}

func (t *tools) story(ctx context.Context, in StoryIn) (*StoryOut, error) {
	project, abs, err := t.locate(in.Project, in.Path)
	if err != nil {
		return nil, err
	}
	sum := strings.TrimPrefix(in.SHA256, "sha256:")
	if sum == "" {
		if sum, err = fileHash(abs); err != nil {
			return nil, fmt.Errorf("%s: %v (give sha256 for a version that is gone)", in.Path, err)
		}
	}
	st := t.sv.Story(project, abs, sum)
	if st == nil {
		return nil, fmt.Errorf("no recorded step wrote %s with sha256 %s", rel(project, abs), short(sum))
	}
	states := t.sv.Placer.StepsInKton(ctx, st)
	out := &StoryOut{File: rel(project, abs), SHA256: sum, Project: project, Repo: st.Repo, Verdict: st.Verdict.Kind, Gaps: st.Gaps, Context: st.Context}
	for _, s := range st.Steps {
		key := s.Session + "/" + s.Task // a change by hand: hand/<file>@<sha256 prefix>, as the story tab keys it
		out.Steps = append(out.Steps, StoryStep{Key: key, When: stamp(s.When), Where: s.Where, Sentence: s.Sentence,
			Code: clip(s.Code, 600), OnPath: s.OnPath, InKton: states[s.Session+"/"+s.Task], Hand: s.Hand, Warnings: s.Warnings, Note: t.stepNote(s.Session, s.Task, s.Where)})
	}
	return out, nil
}

// ------------------------------------------------------------------ ask

type AskIn struct {
	Query   string `json:"query" jsonschema:"producer, uses, lineage, about, record or reproductions"`
	Ref     string `json:"ref" jsonschema:"a file (absolute or relative to the project: its bytes are asked about) or sha256:<hash> (a file's bytes or a record id)"`
	Project string `json:"project,omitempty" jsonschema:"whose repository asks (its federation and trust tiers decide what is seen); found from ref when it is a path"`
}
type AskOut struct {
	Answer any `json:"answer"`
}

var askable = map[string]bool{"producer": true, "uses": true, "lineage": true, "about": true, "record": true, "reproductions": true}

func (t *tools) ask(ctx context.Context, in AskIn) (*AskOut, error) {
	if !askable[in.Query] {
		return nil, fmt.Errorf("query %q: one of producer, uses, lineage, about, record, reproductions", in.Query)
	}
	ref := in.Ref
	project := in.Project
	if !strings.HasPrefix(ref, "sha256:") {
		p, abs, err := t.locate(in.Project, ref)
		if err != nil {
			return nil, err
		}
		sum, err := fileHash(abs)
		if err != nil {
			return nil, err
		}
		project, ref = p, "sha256:"+sum
	}
	if project == "" {
		return nil, fmt.Errorf("project is needed with a sha256 reference")
	}
	repo := watch.RepoRoot(project)
	ask := watch.CockpitAsk
	if t.sv.Placer != nil {
		ask = t.sv.Placer.Ask
	}
	// a repository reading big sources answers slowly today (an ask about one record of kton-web's
	// pmx corpus took 50 s); two minutes, the cockpit's own ceiling for a query
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	b, err := ask(ctx, repo, map[string]string{"query": in.Query, "ref": ref})
	if err != nil {
		return nil, fmt.Errorf("cockpit ask %s in %s: %v", in.Query, repo, err)
	}
	var a any
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &AskOut{Answer: a}, nil
}

// ------------------------------------------------------------------ sparql

// sparqlHelp teaches the dataset and gives worked examples (the cockpit's docs/sparql.md, where
// each runs in a test; the two model-development ones follow the same rule for templates).
const sparqlHelp = `A SPARQL SELECT or ASK of your own over kton, as the project's repository sees it: every step (foton) of the repository and its federation sources, attributed to a signer only where a key its trust tiers trust verified it; every claim a trusted key signed. The cockpit refuses anything but SELECT/ASK, SERVICE and FROM. Prefer kton_ask (producer, uses, lineage, about, record) for one file or record; use this for questions across many. This is for EXPLORING: orienting, finding, comparing. Nothing signed rests on its answer. A result someone relies on (evidence, a check, a number in a report) is a SPARQL step in plankton instead: the query and its input as files, its answer an output file, run without cache and published like any step; propose that to the person when an answer matters.

Needs a cockpit built with SPARQL (kton's RDF projection; until kton 0.3 a build with -tags unreleased and an engine): otherwise the answer is a refusal "sparql.unavailable" — say so to the person and fall back to kton_ask.

Prefixes: prov: <http://www.w3.org/ns/prov#> rdfs: <http://www.w3.org/2000/01/rdf-schema#> dct: <http://purl.org/dc/terms/> ep: <https://w3id.org/ep-plan#> np: <http://www.nanopub.org/nschema#> nk: <https://kton.dev/v/> xsd: <http://www.w3.org/2001/XMLSchema#>
Nodes: a file or record sha256:<hex> is <https://kton.dev/o/<hex>>; a signer is <https://kton.dev/agent/<keyid>> (16 hex).
A step (default graph): ?step a prov:Activity ; prov:used ?input ; nk:protocolRef ?hash ; prov:wasAttributedTo ?signer . ?output prov:wasGeneratedBy ?step ; prov:wasDerivedFrom ?input . ?file rdfs:label "data/in.csv" (the path the step named it by).
A claim is a nanopublication in four graphs: GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo } GRAPH ?assertion { ?subject ?predicate ?o . ?o ?field ?value } GRAPH ?pubinfo { ?np dct:created ?when ; nk:scope ?scope } GRAPH ?attribution { ?np dct:creator ?signer }. describes is dct:description, rationale is ep:hasRationale, both with field nk:lab/value. Other templates under their own predicate with fields nk:lab/<field>: the model development is <https://kton.dev/v/pmx/parent-step> (field nk:lab/parent-step: the parent step's id as a string), pmx/based-on, pmx/key-step, pmx/base-model, pmx/final-model, pmx/qc-passed (field nk:lab/value, often nk:lab/why).
Not in the dataset: a step's time and command text (match input paths; kton_ask record shows the command), a scope's parent (kton_ask about / the story's context), trust tiers (name signer keys).

Examples (replace FILE, STEP, KEYID with hex):
1. Every step that used a file, and who signed it:
SELECT ?step ?signer WHERE { ?step prov:used <https://kton.dev/o/FILE> . OPTIONAL { ?step prov:wasAttributedTo ?signer } }
2. Descriptions and rationales of a step:
SELECT ?kind ?text ?signer ?when WHERE { VALUES (?predicate ?kind) { (dct:description "description") (ep:hasRationale "rationale") } GRAPH ?assertion { <https://kton.dev/o/STEP> ?predicate ?o . ?o <https://kton.dev/v/lab/value> ?text } GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo } GRAPH ?pubinfo { ?np dct:created ?when } GRAPH ?attribution { ?np dct:creator ?signer } } ORDER BY ?when
3. What one signer said in a time window:
SELECT ?claim ?when WHERE { GRAPH ?attribution { ?np dct:creator <https://kton.dev/agent/KEYID> } GRAPH ?pubinfo { ?np dct:created ?when ; prov:wasDerivedFrom ?claim } FILTER (?when >= "2026-01-01T00:00:00Z"^^xsd:dateTime) } ORDER BY ?when
4. The claims in a scope, by its name (e.g. a study):
SELECT ?claim ?subject ?predicate WHERE { GRAPH ?seedAssertion { ?s a nk:Scope ; rdfs:label "study-thp-101" } GRAPH ?seedHead { ?seed np:hasAssertion ?seedAssertion ; np:hasPublicationInfo ?seedInfo } GRAPH ?seedInfo { ?seed nk:genesis true ; nk:scope ?scope } GRAPH ?pubinfo { ?np nk:scope ?scope ; prov:wasDerivedFrom ?claim FILTER NOT EXISTS { ?np nk:genesis true } } GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo } GRAPH ?assertion { ?subject ?predicate ?o FILTER NOT EXISTS { ?any ?points ?subject } } }
5. Files two independent signers produced:
SELECT DISTINCT ?file ?path WHERE { ?file prov:wasGeneratedBy ?one, ?other . ?one prov:wasAttributedTo ?a . ?other prov:wasAttributedTo ?b . FILTER (?one != ?other && ?a != ?b) OPTIONAL { ?file rdfs:label ?path } }
6. Steps that ran a script, by its path (how did others fit a model?):
SELECT DISTINCT ?step ?path WHERE { ?step prov:used ?script ; prov:wasAttributedTo ?signer . ?script rdfs:label ?path . FILTER regex(?path, "fit|run0", "i") }
7. Everything a file was derived from:
SELECT DISTINCT ?source ?path WHERE { <https://kton.dev/o/FILE> prov:wasDerivedFrom+ ?source . OPTIONAL { ?source rdfs:label ?path } }
8. Did a trusted key produce this file? ASK { <https://kton.dev/o/FILE> prov:wasGeneratedBy ?step . ?step prov:wasAttributedTo ?signer }
9. Final and base models, and why (model development; same rule, not in the cockpit's tests):
SELECT ?step ?kind ?model ?why ?signer WHERE { VALUES (?p ?kind) { (<https://kton.dev/v/pmx/final-model> "final") (<https://kton.dev/v/pmx/base-model> "base") } GRAPH ?assertion { ?step ?p ?o . ?o <https://kton.dev/v/lab/value> ?model OPTIONAL { ?o <https://kton.dev/v/lab/why> ?why } } GRAPH ?head { ?np np:hasAssertion ?assertion } GRAPH ?attribution { ?np dct:creator ?signer } }
10. A model development: each step and the step it was developed from (same rule, not in the cockpit's tests):
SELECT ?child ?parent ?why WHERE { GRAPH ?assertion { ?child <https://kton.dev/v/pmx/parent-step> ?o . ?o <https://kton.dev/v/lab/parent-step> ?parent OPTIONAL { ?o <https://kton.dev/v/lab/why> ?why } } }
Write the PREFIX lines you use at the top of the query.`

type SparqlIn struct {
	Sparql         string `json:"sparql" jsonschema:"a SELECT or ASK, with its PREFIX lines"`
	Project        string `json:"project" jsonschema:"whose repository asks: its federation and trust tiers decide what is seen (kton_projects)"`
	Limit          int    `json:"limit,omitempty" jsonschema:"rows at most (cockpit default 1000, max 10000)"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" jsonschema:"seconds at most (cockpit default 30, max 120)"`
}

// askJSON is how the tools ask the cockpit with any request (tests replace it).
var askJSON = watch.CockpitAskJSON

func (t *tools) sparql(ctx context.Context, in SparqlIn) (*AskOut, error) {
	if strings.TrimSpace(in.Sparql) == "" || in.Project == "" {
		return nil, fmt.Errorf("sparql and project are needed")
	}
	req := map[string]any{"query": "sparql", "sparql": in.Sparql}
	if in.Limit > 0 {
		req["limit"] = in.Limit
	}
	wait := 2 * time.Minute // the cockpit's default is 30 s for the query, but the work around it can take longer
	if in.TimeoutSeconds > 0 {
		req["timeoutSeconds"] = in.TimeoutSeconds
		wait = time.Duration(in.TimeoutSeconds+5) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	repo := watch.RepoRoot(in.Project)
	b, err := askJSON(ctx, repo, req)
	if err != nil {
		if strings.Contains(err.Error(), `unknown field "sparql"`) || strings.Contains(err.Error(), "sparql.unavailable") {
			return nil, fmt.Errorf("this cockpit cannot answer SPARQL yet (it needs ask sparql, kton-cockpit #38, built with kton's RDF projection); answer with kton_ask instead. The cockpit said: %v", err)
		}
		return nil, fmt.Errorf("cockpit ask sparql in %s: %v", repo, err)
	}
	var a struct {
		Sparql any `json:"sparql"`
	}
	if err := json.Unmarshal(b, &a); err != nil || a.Sparql == nil {
		var whole any
		json.Unmarshal(b, &whole)
		return &AskOut{Answer: whole}, nil
	}
	return &AskOut{Answer: a.Sparql}, nil
}

// ------------------------------------------------------------------ note

type NoteIn struct {
	Key         string `json:"key" jsonschema:"the step: session/task"`
	Description string `json:"description,omitempty" jsonschema:"what the step does, in the person's terms"`
	Rationale   string `json:"rationale,omitempty" jsonschema:"why it was done this way"`
}
type NoteOut struct {
	Key  string      `json:"key"`
	Note watch.Note  `json:"note"`
	Was  *watch.Note `json:"was,omitempty"`
}

func (t *tools) note(_ context.Context, in NoteIn) (*NoteOut, error) {
	if t.sv.Notes == nil {
		return nil, fmt.Errorf("no note store (ktonwatch has no -store)")
	}
	if in.Key == "" || !strings.Contains(in.Key, "/") {
		return nil, fmt.Errorf("key %q: a step key session/task, from kton_runs or kton_story", in.Key)
	}
	if !strings.HasPrefix(in.Key, "hand/") {
		sess, task, _ := strings.Cut(in.Key, "/")
		found := false
		t.sv.With(sess, func(s *watch.Session) {
			for _, tk := range s.Tasks {
				found = found || tk.ID == task
			}
		})
		if !found {
			return nil, fmt.Errorf("no recorded step %s", in.Key)
		}
	}
	was := t.noteOf(in.Key)
	n := watch.Note{Description: strings.TrimSpace(in.Description), Rationale: strings.TrimSpace(in.Rationale), By: "claude"}
	if err := t.sv.Notes.Put(in.Key, n); err != nil {
		return nil, err
	}
	got, _ := t.sv.Notes.Get(in.Key)
	return &NoteOut{Key: in.Key, Note: got, Was: was}, nil
}

// ------------------------------------------------------------------ the editor (watch/ui.go)

type EditorOut struct {
	OK      bool           `json:"ok"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (t *tools) editor(ctx context.Context, wait time.Duration, action string, args map[string]any) (*EditorOut, error) {
	if t.sv.UI == nil {
		return nil, watch.ErrNoEditor
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	a, err := t.sv.UI.Do(ctx, action, args)
	if err != nil {
		return nil, err
	}
	return &EditorOut{OK: a.OK, Message: a.Message, Data: a.Data}, nil
}

// fileArgs: project, absolute path and version of a file named by path (and optional sha)
func (t *tools) fileArgs(project, path, sha string) (map[string]any, error) {
	project, abs, err := t.locate(project, path)
	if err != nil {
		return nil, err
	}
	sum := strings.TrimPrefix(sha, "sha256:")
	if sum == "" {
		if sum, err = fileHash(abs); err != nil {
			return nil, fmt.Errorf("%s: %v (give sha256 for a version that is gone)", path, err)
		}
	}
	return map[string]any{"project": project, "path": abs, "sha256": sum}, nil
}

type OpenStoryIn struct {
	Path    string `json:"path" jsonschema:"the file, absolute or relative to the project"`
	Project string `json:"project,omitempty"`
	SHA256  string `json:"sha256,omitempty" jsonschema:"a version; the bytes on disk today when left out"`
}

func (t *tools) openStory(ctx context.Context, in OpenStoryIn) (*EditorOut, error) {
	args, err := t.fileArgs(in.Project, in.Path, in.SHA256)
	if err != nil {
		return nil, err
	}
	return t.editor(ctx, 30*time.Second, "open_story", args)
}

type OpenGraphIn struct {
	Path    string `json:"path,omitempty" jsonschema:"the file to centre on, absolute or relative to the project"`
	Hash    string `json:"hash,omitempty" jsonschema:"or a file hash / record id (sha256:…) to centre on"`
	Project string `json:"project,omitempty" jsonschema:"the project whose graph; needed with hash"`
	Repo    string `json:"repo,omitempty" jsonschema:"instead of a project: a cockpit repository to show (e.g. an explore repository that reads everything), with hash; only signed records, no recording"`
	Title   string `json:"title,omitempty" jsonschema:"the tab's title, e.g. what is shown"`
}

func (t *tools) openGraph(ctx context.Context, in OpenGraphIn) (*EditorOut, error) {
	if in.Repo != "" {
		if in.Hash == "" {
			return nil, fmt.Errorf("repo needs hash (a file hash or record id to centre on)")
		}
		id, err := t.sv.AllowRegistry(in.Repo)
		if err != nil {
			return nil, err
		}
		return t.editor(ctx, 30*time.Second, "open_graph", map[string]any{"project": in.Repo, "registry": id,
			"sha256": strings.TrimPrefix(in.Hash, "sha256:"), "title": in.Title})
	}
	var args map[string]any
	switch {
	case in.Path != "":
		a, err := t.fileArgs(in.Project, in.Path, "")
		if err != nil {
			return nil, err
		}
		args = a
	case in.Hash != "" && in.Project != "":
		args = map[string]any{"project": in.Project, "sha256": strings.TrimPrefix(in.Hash, "sha256:")}
	default:
		return nil, fmt.Errorf("path, or hash and project")
	}
	return t.editor(ctx, 30*time.Second, "open_graph", args)
}

type SelectIn struct {
	Project string   `json:"project" jsonschema:"the project whose graph, or the repo a graph was opened on (kton_open_graph repo)"`
	Items   []string `json:"items" jsonschema:"files (paths relative to the project or absolute), file hashes or record ids (sha256:…); empty clears"`
}

func (t *tools) selectSteps(ctx context.Context, in SelectIn) (*EditorOut, error) {
	if in.Project == "" {
		return nil, fmt.Errorf("project is needed")
	}
	sel := []string{}
	for _, it := range in.Items {
		if strings.HasPrefix(it, "sha256:") {
			sel = append(sel, it)
			continue
		}
		_, abs, err := t.locate(in.Project, it)
		if err != nil {
			return nil, err
		}
		sum, err := fileHash(abs)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", it, err)
		}
		sel = append(sel, "sha256:"+sum)
	}
	return t.editor(ctx, 30*time.Second, "select", map[string]any{"project": in.Project, "selection": sel})
}

type SelectionIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project whose graph; the one open when left out"`
}

func (t *tools) selection(ctx context.Context, in SelectionIn) (*EditorOut, error) {
	return t.editor(ctx, 15*time.Second, "get_selection", map[string]any{"project": in.Project})
}

type CopyIn struct {
	Hash    string `json:"hash" jsonschema:"the file's content hash, sha256:…"`
	Name    string `json:"name,omitempty" jsonschema:"its name where it comes from (e.g. R/04_fit_population.R), for the person and as the default target"`
	Project string `json:"project" jsonschema:"the project to copy into"`
	To      string `json:"to,omitempty" jsonschema:"the target path relative to the project; the name when left out"`
}

func (t *tools) copyFile(ctx context.Context, in CopyIn) (*EditorOut, error) {
	sum := strings.TrimPrefix(in.Hash, "sha256:")
	if len(sum) != 64 || in.Project == "" {
		return nil, fmt.Errorf("hash (sha256:<64 hex>) and project are needed")
	}
	to := in.To
	if to == "" {
		to = in.Name
	}
	if to == "" || filepath.IsAbs(to) || strings.HasPrefix(filepath.Clean(to), "..") {
		return nil, fmt.Errorf("to: a path inside the project (or name)")
	}
	// the person decides: up to three minutes
	return t.editor(ctx, 3*time.Minute, "copy", map[string]any{"project": in.Project, "sha256": sum, "name": in.Name, "to": to})
}

// ------------------------------------------------------------------ helpers

func (t *tools) noteOf(key string) *watch.Note {
	if t.sv.Notes == nil {
		return nil
	}
	if n, ok := t.sv.Notes.Get(key); ok {
		return &n
	}
	return nil
}

// stepNote is a step's draft: under its key (session/task), else where a story line written before
// keys were per step kept it: session/<where>, a chunk's "chunk #n of " left out (story.js chapters).
var chunkOf = regexp.MustCompile(`^chunk \S+ of `)

func (t *tools) stepNote(session, task, where string) *watch.Note {
	if n := t.noteOf(session + "/" + task); n != nil {
		return n
	}
	return t.noteOf(session + "/" + chunkOf.ReplaceAllString(where, ""))
}

// locate: the project a path belongs to and the path made absolute
func (t *tools) locate(project, path string) (string, string, error) {
	if path == "" {
		return "", "", fmt.Errorf("path is needed")
	}
	if project != "" && !filepath.IsAbs(path) {
		return project, filepath.Join(project, path), nil
	}
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("%s is relative: give the project too, or an absolute path", path)
	}
	if project != "" {
		return project, path, nil
	}
	best := ""
	for _, p := range t.sv.Projects() {
		if strings.HasPrefix(path, p.Path+"/") && len(p.Path) > len(best) {
			best = p.Path
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("%s is in no recorded project", path)
	}
	return best, path, nil
}

func relTo(project string) func(string) string {
	return func(p string) string { return rel(project, p) }
}

func rel(project, p string) string {
	if r, err := filepath.Rel(project, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func stamp(ns int64) string {
	if ns == 0 {
		return ""
	}
	return time.Unix(0, ns).Format("2006-01-02 15:04:05")
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + " …"
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
