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
			"story when publishing. Nothing here signs or publishes.",
	})
	mcp.AddTool(s, &mcp.Tool{Name: "kton_projects", Description: "The projects the recorder has sessions for, most recent first, with their cockpit repository (registry) if they are in one."},
		mcpsurface.Handler(t.projects))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_runs", Description: "The recorded sessions of a project (R and Python consoles, terminals) with their steps: key, time, where (script, chunk, console), code, files read and written, and the draft note if there is one. Steps and sessions without effect are left out."},
		mcpsurface.Handler(t.runs))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_story", Description: "How a file came about: every step from the start of its sessions up to the file, on_path marking those it was made from, whether kton already has each step (kton: the same computation is published; same: the same output bytes are published but from other inputs; empty: not in kton), the draft notes, and where its source data resides (study › project › programme)."},
		mcpsurface.Handler(t.story))
	mcp.AddTool(s, &mcp.Tool{Name: "kton_ask", Description: "Ask kton (the cockpit, through the project's repository and its federation sources, verified against its trust tiers) about a file or a record: producer (who made these bytes), uses (who used them), lineage (the chain behind them), about (the claims: descriptions, rationales), record (what a record says), reproductions."},
		mcpsurface.Handler(t.ask))
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
				st.Note = t.noteOf(st.Key)
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
			Code: clip(s.Code, 600), OnPath: s.OnPath, InKton: states[s.Session+"/"+s.Task], Hand: s.Hand, Warnings: s.Warnings, Note: t.noteOf(key)})
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
