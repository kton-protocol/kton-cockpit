package watch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The story editor's store (MH 20./21.11.): what a person corrects in a recorded step before it
// is published, and the steps they declare without a recording, by hand or through a workspace
// heuristic. The recording itself never changes: an edit is an overlay that the draft of a step
// is read through, so every reader of /api/draft (Publish, the plan preview, New run, the MCP
// tools) sees the corrected step. A declared step is published with `declared` (kton-cockpit
// v0.5.11: hand | heuristic:<name>), so kton tells it from a recorded one (nk:declared, kton v0.4.3).

// StepEdit corrects one recorded step (key "<session>/<task>"). A nil list leaves the draft's own
// choice; a list replaces it: exactly these files are used, in this order.
type StepEdit struct {
	Inputs  *[]string `json:"inputs,omitempty"`  // repo-relative paths
	Outputs *[]string `json:"outputs,omitempty"` // repo-relative paths
	Cmd     *string   `json:"cmd,omitempty"`
	By      string    `json:"by,omitempty"` // who changed it last: "you", "claude", …
	When    int64     `json:"when_ns,omitempty"`
}

func (e StepEdit) empty() bool { return e.Inputs == nil && e.Outputs == nil && e.Cmd == nil }

// PickedFile is a file of a declared step, with the bytes it had when it was picked.
type PickedFile struct {
	Path   string `json:"path"`   // repo-relative
	SHA256 string `json:"sha256"` // hex, as picked
}

// DeclaredStep is a step nobody recorded: picked by hand in the editor, or proposed by a heuristic
// and kept by the person.
type DeclaredStep struct {
	ID       string       `json:"id"`
	Project  string       `json:"project"`  // the recorded project (a folder on the watched list)
	Declared string       `json:"declared"` // "hand" or "heuristic:<name>"
	Inputs   []PickedFile `json:"inputs"`
	Outputs  []PickedFile `json:"outputs"`
	Cmd      string       `json:"cmd"`
	By       string       `json:"by,omitempty"`
	When     int64        `json:"when_ns"`
}

// Key is the step's key in the story, notes and edits: "declared/<id>".
func (d DeclaredStep) Key() string { return DeclaredSession + "/" + d.ID }

// DeclaredSession is the session name the story and the draft API use for declared steps.
const DeclaredSession = "declared"

// DeclaredForm is what `declared` may say (kton-cockpit's publish refuses anything else).
var DeclaredForm = regexp.MustCompile(`^(hand|heuristic:[A-Za-z0-9][A-Za-z0-9._-]*)$`)

// Edits is the store: one JSON file, written whole on every change.
type Edits struct {
	path string
	mu   sync.Mutex
	data editsFile
}

type editsFile struct {
	Steps    map[string]StepEdit `json:"steps"`
	Declared []DeclaredStep      `json:"declared"`
}

// OpenEdits reads the edits under dir (created when the first edit is written).
func OpenEdits(dir string) *Edits {
	e := &Edits{path: filepath.Join(dir, "edits.json"), data: editsFile{Steps: map[string]StepEdit{}}}
	if b, err := os.ReadFile(e.path); err == nil {
		json.Unmarshal(b, &e.data)
	}
	if e.data.Steps == nil {
		e.data.Steps = map[string]StepEdit{}
	}
	return e
}

// Step is the edit of one recorded step.
func (e *Edits) Step(key string) (StepEdit, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.data.Steps[key]
	return v, ok
}

// Steps is every step edit whose key starts with one of the prefixes (all without a prefix).
func (e *Edits) Steps(prefixes ...string) map[string]StepEdit {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]StepEdit{}
	for k, v := range e.data.Steps {
		if len(prefixes) == 0 || hasAnyPrefix(k, prefixes) {
			out[k] = v
		}
	}
	return out
}

// PutStep sets a recorded step's edit; an edit that changes nothing removes it (back to the draft).
func (e *Edits) PutStep(key string, ed StepEdit) error {
	if strings.HasPrefix(key, DeclaredSession+"/") || !strings.Contains(key, "/") {
		return fmt.Errorf("%q is not a recorded step's key (<session>/<task>)", key)
	}
	for _, l := range []*[]string{ed.Inputs, ed.Outputs} {
		if l == nil {
			continue
		}
		for _, p := range *l {
			if err := plainRel(p); err != nil {
				return err
			}
		}
	}
	if ed.When == 0 {
		ed.When = time.Now().UnixNano()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if ed.empty() {
		delete(e.data.Steps, key)
	} else {
		e.data.Steps[key] = ed
	}
	return e.save()
}

// Declared is the declared steps of a project (all of them for "").
func (e *Edits) Declared(project string) []DeclaredStep {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []DeclaredStep
	for _, d := range e.data.Declared {
		if project == "" || d.Project == project {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When < out[j].When })
	return out
}

// DeclaredByID is one declared step.
func (e *Edits) DeclaredByID(id string) (DeclaredStep, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, d := range e.data.Declared {
		if d.ID == id {
			return d, true
		}
	}
	return DeclaredStep{}, false
}

// DeclareRequest is what the editor (or a heuristic the person kept) sends for a new declared step.
type DeclareRequest struct {
	ID       string   `json:"id,omitempty"` // to replace an existing declared step
	Project  string   `json:"project"`
	Declared string   `json:"declared,omitempty"` // default "hand"
	Inputs   []string `json:"inputs"`             // repo-relative
	Outputs  []string `json:"outputs"`            // repo-relative
	Cmd      string   `json:"cmd"`
	By       string   `json:"by,omitempty"`
}

// Declare stores a declared step. Every file is hashed now, from the repository the project
// belongs to: what was picked is what is published, and a later change of a file shows in its
// draft. A step needs at least one output (kton publishes a step by what it produced).
func (e *Edits) Declare(in DeclareRequest) (DeclaredStep, error) {
	if in.Project == "" || !filepath.IsAbs(in.Project) {
		return DeclaredStep{}, errors.New("project is an absolute folder")
	}
	if in.Declared == "" {
		in.Declared = "hand"
	}
	if !DeclaredForm.MatchString(in.Declared) {
		return DeclaredStep{}, fmt.Errorf("declared is hand or heuristic:<name>, not %q", in.Declared)
	}
	if len(in.Outputs) == 0 {
		return DeclaredStep{}, errors.New("a declared step names at least one output")
	}
	if strings.TrimSpace(in.Cmd) == "" {
		return DeclaredStep{}, errors.New("a declared step says what was done (cmd)")
	}
	repo := RepoRoot(in.Project)
	pick := func(ps []string) ([]PickedFile, error) {
		out := []PickedFile{} // never null in the JSON: a step without inputs has none
		seen := map[string]bool{}
		for _, p := range ps {
			if err := plainRel(p); err != nil {
				return nil, err
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			abs, err := inRepo(repo, p)
			if err != nil {
				return nil, err
			}
			sum, err := hashOf(abs)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", p, err)
			}
			out = append(out, PickedFile{Path: p, SHA256: sum})
		}
		return out, nil
	}
	ins, err := pick(in.Inputs)
	if err != nil {
		return DeclaredStep{}, err
	}
	outs, err := pick(in.Outputs)
	if err != nil {
		return DeclaredStep{}, err
	}
	for _, o := range outs {
		for _, i := range ins {
			if o.Path == i.Path {
				return DeclaredStep{}, fmt.Errorf("%s is both an input and an output", o.Path)
			}
		}
	}
	d := DeclaredStep{ID: in.ID, Project: in.Project, Declared: in.Declared, Inputs: ins, Outputs: outs, Cmd: strings.TrimSpace(in.Cmd), By: in.By, When: time.Now().UnixNano()}
	if d.By == "" {
		d.By = "you"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if d.ID == "" {
		d.ID = newID()
	} else {
		found := false
		for i, x := range e.data.Declared {
			if x.ID == d.ID {
				if x.Project != d.Project {
					return DeclaredStep{}, fmt.Errorf("declared step %s belongs to %s", d.ID, x.Project)
				}
				e.data.Declared[i], found = d, true
			}
		}
		if !found {
			return DeclaredStep{}, fmt.Errorf("no declared step %s", d.ID)
		}
		return d, e.save()
	}
	e.data.Declared = append(e.data.Declared, d)
	return d, e.save()
}

// Undeclare removes a declared step.
func (e *Edits) Undeclare(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, d := range e.data.Declared {
		if d.ID == id {
			e.data.Declared = append(e.data.Declared[:i], e.data.Declared[i+1:]...)
			return e.save()
		}
	}
	return fmt.Errorf("no declared step %s", id)
}

func (e *Edits) save() error {
	if err := os.MkdirAll(filepath.Dir(e.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e.data, "", " ")
	if err != nil {
		return err
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

// ApplyEdit is a recorded step's draft as the person corrected it. Files the person added that the
// recording did not see are named as such; files they left out stay listed, unused.
//
// What is signed must not claim more observation than there was (kton-review D1 on #236): a
// correction that only leaves out or takes in files the recorder saw the step read or write is
// still an observation, narrowed by the person. A correction that adds a file the recorder never
// saw, or changes what was done, is the person's account: the step is published as declared=hand.
func ApplyEdit(d Draft, ed StepEdit) Draft {
	if ed.empty() {
		return d
	}
	d.Edited = true
	added := false
	if ed.Inputs != nil {
		d.Inputs, added = chooseNoting(d.Inputs, *ed.Inputs, added)
	}
	if ed.Outputs != nil {
		d.Outputs, added = chooseNoting(d.Outputs, *ed.Outputs, added)
	}
	if ed.Cmd != nil && *ed.Cmd != d.Cmd {
		d.Cmd = *ed.Cmd
		added = true
	}
	if added {
		d.Declared = "hand"
	}
	return d
}

func chooseNoting(have []DraftFile, want []string, added bool) ([]DraftFile, bool) {
	seen := map[string]bool{}
	for _, f := range have {
		seen[f.Path] = true
	}
	for _, p := range want {
		if !seen[p] {
			added = true
		}
	}
	return choose(have, want), added
}

// choose marks exactly the given paths as used: in their order first, then the rest, unused.
func choose(have []DraftFile, want []string) []DraftFile {
	byPath := map[string]DraftFile{}
	for _, f := range have {
		byPath[f.Path] = f
	}
	var out []DraftFile
	used := map[string]bool{}
	for _, p := range want {
		if used[p] {
			continue
		}
		used[p] = true
		f, ok := byPath[p]
		if !ok {
			f = DraftFile{Path: p, Note: "added by hand: the recording did not see it"}
		}
		f.Use = true
		out = append(out, f)
	}
	for _, f := range have {
		if !used[f.Path] {
			f.Use = false
			out = append(out, f)
		}
	}
	return out
}

// DeclaredDraft is a declared step's draft: its picked files, each checked against its bytes now.
// A file that changed since it was picked is not used: publish would sign other bytes.
func DeclaredDraft(d DeclaredStep) Draft {
	repo := RepoRoot(d.Project)
	out := Draft{Repo: repo, Cmd: d.Cmd, Declared: d.Declared}
	file := func(f PickedFile) DraftFile {
		x := DraftFile{Path: f.Path, Use: true}
		abs, err := inRepo(repo, f.Path)
		if err != nil {
			x.Use, x.Note = false, err.Error()
			return x
		}
		now, err := hashOf(abs)
		switch {
		case err != nil:
			x.Use, x.Note = false, "gone since it was picked"
		case now != f.SHA256:
			x.Use, x.Note = false, "changed since it was picked: pick it again"
		}
		return x
	}
	for _, f := range d.Inputs {
		out.Inputs = append(out.Inputs, file(f))
	}
	for _, f := range d.Outputs {
		out.Outputs = append(out.Outputs, file(f))
	}
	return out
}

// plainRel: a repository-relative path, never absolute and never leaving the repository.
func plainRel(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || strings.ContainsAny(p, "\\\x00") {
		return fmt.Errorf("%q is not a repository-relative path", p)
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." {
			return fmt.Errorf("%q is not a plain repository-relative path", p)
		}
	}
	return nil
}

// inRepo: the file a repository-relative path names, with every symlink resolved, refused when it
// resolves outside the repository (kton-review S1 on #236: a link in the repo to a file outside it
// would have its outside bytes declared as the repo's).
func inRepo(repo, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	abs, err := filepath.EvalSymlinks(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("%s: %v", rel, err)
	}
	if r, err := filepath.Rel(root, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("%s resolves outside the repository", rel)
	}
	return abs, nil
}

func hashOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.IsDir() {
		return "", errors.New("is a folder, not a file")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hasAnyPrefix(k string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// isWatched: a declared step belongs to a folder the recorder records (or any folder when nothing
// restricts it, as for a ktonwatch without a watched list).
func (sv *Service) isWatched(project string) bool {
	if project == "" || !filepath.IsAbs(project) {
		return false
	}
	if sv.Watched == nil {
		return true
	}
	return sv.Watched.Covers(project)
}

// withDeclared adds the project's declared steps that lead to the story's file: one whose output
// is the file (with the bytes the story is about), or a file a step of the story read, or an input
// of a declared step already added. A file nobody recorded writing still has a story when a
// declared step produced it. A declared step stands before the first step that reads what it made.
func withDeclared(st *Story, project, path, sum string, decl []DeclaredStep) *Story {
	if len(decl) == 0 {
		return st
	}
	repo := RepoRoot(project)
	rel := func(p string) string { // repo-relative to the story's (project-relative) form
		abs := filepath.Join(repo, filepath.FromSlash(p))
		if r, err := filepath.Rel(project, abs); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return abs
	}
	target := path
	if r, err := filepath.Rel(project, path); err == nil && !strings.HasPrefix(r, "..") {
		target = filepath.ToSlash(r)
	}
	sum = strings.TrimPrefix(sum, "sha256:")
	if st == nil {
		var made *DeclaredStep
		for i, d := range decl {
			for _, o := range d.Outputs {
				if rel(o.Path) == target && (sum == "" || o.SHA256 == sum) {
					made = &decl[i]
				}
			}
		}
		if made == nil {
			return nil
		}
		st = &Story{Target: FileVersion{Path: path, SHA256: made.outputSum(rel, target), Role: "result", When: made.When},
			Verdict: Verdict{Kind: "declared"}, Repo: repo}
	}
	wanted := map[string]bool{target: true}
	firstRead := map[string]int64{}
	for _, s := range st.Steps {
		for _, r := range s.Reads {
			wanted[r] = true
			if w, ok := firstRead[r]; !ok || s.When < w {
				firstRead[r] = s.When
			}
		}
	}
	added := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, d := range decl {
			if added[d.ID] {
				continue
			}
			hit := false
			for _, o := range d.Outputs {
				if wanted[rel(o.Path)] {
					hit = true
				}
			}
			if !hit {
				continue
			}
			added[d.ID], changed = true, true
			step := declaredStoryStep(d, rel)
			when := st.Target.When
			for _, o := range step.Writes {
				if w, ok := firstRead[o]; ok && (when == 0 || w <= when) {
					when = w - 1
				}
			}
			if when == 0 {
				when = d.When
			}
			step.When = when
			for _, r := range step.Reads {
				wanted[r] = true
				if w, ok := firstRead[r]; !ok || when < w {
					firstRead[r] = when
				}
			}
			st.Steps = append(st.Steps, step)
		}
	}
	sort.SliceStable(st.Steps, func(i, j int) bool { return st.Steps[i].When < st.Steps[j].When })
	return st
}

func (d DeclaredStep) outputSum(rel func(string) string, target string) string {
	for _, o := range d.Outputs {
		if rel(o.Path) == target {
			return o.SHA256
		}
	}
	return ""
}

// declaredStoryStep is a declared step as the story tells it.
func declaredStoryStep(d DeclaredStep, rel func(string) string) StoryStep {
	where := "declared by hand"
	if strings.HasPrefix(d.Declared, "heuristic:") {
		where = "reconstructed by " + strings.TrimPrefix(d.Declared, "heuristic:")
	}
	s := StoryStep{Session: DeclaredSession, Task: d.ID, Where: where, Sentence: d.Cmd, Code: d.Cmd, OnPath: true, Declared: d.Declared}
	for _, f := range d.Inputs {
		s.Reads = append(s.Reads, rel(f.Path))
		s.InHashes = append(s.InHashes, f.SHA256)
	}
	for _, f := range d.Outputs {
		s.Writes = append(s.Writes, rel(f.Path))
		s.Hashes = append(s.Hashes, f.SHA256)
	}
	return s
}
