package watch

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// File-first questions over a whole project (all sessions):
//   - which files are there, and which of them are results
//   - Recipe: what was done to get this file, in order, and what it cannot account for
//   - Descendants: what became of a file someone sent

// StepRef names one task in one session.
type StepRef struct {
	Session string `json:"session"`
	Task    string `json:"task"`
}

// FileVersion is a file with one content.
type FileVersion struct {
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Class      Class     `json:"class"`
	Now        string    `json:"now"`
	Role       string    `json:"role"` // source (never produced here) | intermediate | result (produced, read by nothing later)
	ProducedBy *StepRef  `json:"produced_by,omitempty"`
	ReadBy     []StepRef `json:"read_by,omitempty"`
	When       int64     `json:"when_ns"` // produced, or first read
}

// projectTasks: every task of the project, noise removed, annotated, sessions linked, in
// time order. Caller holds the read lock.
func (sv *Service) projectTasks(check *Checker, project string) []*Task {
	var all []*Task
	f := Filter{HideNoise: true}
	for id, tr := range sv.sessions {
		s := tr.s
		if s.Project != project {
			continue
		}
		for _, t := range sv.annotated(check, s, f.View(s)) {
			t.Session = id
			all = append(all, t)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].End < all[j].End })
	markLater(all)
	return all
}

func vkey(path, sum string) string { return path + "\x00" + sum }

// Files lists the file versions of a project, newest first.
func (sv *Service) Files(project string) []FileVersion {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return filesOf(sv.projectTasks(NewChecker(), project))
}

func filesOf(tasks []*Task) []FileVersion {
	by := map[string]*FileVersion{}
	var order []string
	get := func(p, sum string, cls Class, now string, when int64) *FileVersion {
		k := vkey(p, sum)
		if f, ok := by[k]; ok {
			return f
		}
		f := &FileVersion{Path: p, SHA256: sum, Class: cls, Now: now, When: when}
		by[k] = f
		order = append(order, k)
		return f
	}
	for _, t := range tasks {
		ref := StepRef{t.Session, t.ID}
		for _, in := range t.Inputs {
			f := get(in.Path, in.SHA256, in.Class, in.Now, t.End)
			f.ReadBy = append(f.ReadBy, ref)
		}
		for _, o := range t.Outputs {
			f := get(o.Path, o.SHA256, o.Class, o.Now, t.End)
			r := ref
			f.ProducedBy, f.When = &r, t.End
		}
	}
	var out []FileVersion
	for _, k := range order {
		f := by[k]
		switch {
		case f.ProducedBy == nil:
			f.Role = "source"
		case len(f.ReadBy) == 0:
			f.Role = "result"
		default:
			f.Role = "intermediate"
		}
		out = append(out, *f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When > out[j].When })
	return out
}

// Recipe is how one file version came about.
type Recipe struct {
	Target  FileVersion `json:"target"`
	Steps   []*Task     `json:"steps"`   // only what led to the target, in time order
	Sources []*Input    `json:"sources"` // where it started: files nothing recorded produced
	Gaps    []string    `json:"gaps"`    // what the recording cannot account for
	Detours int         `json:"detours"` // other steps in the same time span: the paths not taken
	Script  string      `json:"script"`  // the steps' code in order, to redo it
	Verdict Verdict     `json:"verdict"`
	Node    string      `json:"node"` // the target's producing step in the kton-web graph form
}

// Verdict: can the file be made again by running scripts the person knows?
type Verdict struct {
	Kind        string      `json:"kind"`              // scripts | scripts-with-gaps | scripts-need-session | interactive
	Scripts     []ScriptRun `json:"scripts,omitempty"` // the script runs among the steps, in order
	Command     string      `json:"command,omitempty"` // how to redo it when Kind is scripts
	SessionVars []VarUse    `json:"session_vars,omitempty"`
	Interactive int         `json:"interactive"` // steps typed or sent from the editor, not scripts
	Notes       []string    `json:"notes,omitempty"`
}

// ScriptRun is one run of an R script: source("x.R") in a session or Rscript x.R.
type ScriptRun struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"` // as it was read when it ran
	Now    string `json:"now"`
	Step   int    `json:"step"` // 1-based index in Steps
}

// scriptOf: the R script a step ran, if it ran one.
func scriptOf(t *Task) *Input {
	if t.Script != "" {
		return &Input{Path: t.Script, SHA256: t.ScriptSHA, Class: ClassProject, Now: NewChecker().State(t.Script, t.ScriptSHA)}
	}
	byPath := map[string]*Input{}
	for _, in := range t.Inputs {
		byPath[in.Path] = in
	}
	for _, s := range t.Sources {
		if s.Class == ClassProject {
			if in := byPath[s.Path]; in != nil {
				return in
			}
		}
	}
	for _, c := range t.Commands { // Rscript x.R, R -f x.R from system()
		if n := c.Name(); n != "Rscript" && n != "R" {
			continue
		}
		for _, a := range splitCmdline(c.Cmdline)[1:] {
			a = strings.TrimPrefix(a, "--file=")
			if !strings.HasSuffix(strings.ToLower(a), ".r") {
				continue
			}
			for _, in := range t.Inputs {
				if in.Class == ClassProject && filepath.Base(in.Path) == filepath.Base(a) {
					return in
				}
			}
		}
	}
	return nil
}

func verdictOf(r *Recipe, rel func(string) string) Verdict {
	v := Verdict{Kind: "scripts"}
	isScript := map[string]bool{}
	for i, t := range r.Steps {
		if in := scriptOf(t); in != nil {
			isScript[stepKey(t)] = true
			// one session running one script is one run, however many commands it has
			if n := len(v.Scripts); n > 0 && v.Scripts[n-1].Path == in.Path && v.Scripts[n-1].SHA256 == in.SHA256 && t.Script != "" {
				continue
			}
			v.Scripts = append(v.Scripts, ScriptRun{Path: in.Path, SHA256: in.SHA256, Now: in.Now, Step: i + 1})
		} else {
			v.Interactive++
		}
	}
	for _, t := range r.Steps {
		if !isScript[stepKey(t)] {
			continue
		}
		for _, u := range t.Uses {
			if !isScript[t.Session+"\x00"+u.Task] {
				v.SessionVars = append(v.SessionVars, u)
			}
		}
	}
	handEdited := false
	for _, src := range r.Sources {
		handEdited = handEdited || src.Status == "changed-outside"
	}
	switch {
	case v.Interactive > 0:
		v.Kind = "interactive"
	case len(v.SessionVars) > 0:
		v.Kind = "scripts-need-session"
	case handEdited:
		v.Kind = "scripts-with-gaps"
		v.Notes = append(v.Notes, "a file was changed by hand between the scripts: the scripts alone do not give the same result")
	}
	var cmds []string
	for _, s := range v.Scripts {
		cmds = append(cmds, "Rscript "+rel(s.Path))
		switch s.Now {
		case NowChanged:
			v.Notes = append(v.Notes, rel(s.Path)+" has changed since the run: run today, it computes something else")
		case NowGone:
			v.Notes = append(v.Notes, rel(s.Path)+" no longer exists")
		}
	}
	if v.Kind == "scripts" || v.Kind == "scripts-with-gaps" {
		v.Command = strings.Join(cmds, " && ")
	}
	return v
}

func stepKey(t *Task) string { return t.Session + "\x00" + t.ID }

// Recipe walks back from a file version through files (From) and variables (Uses).
// sum may be empty: the latest version of the path.
func (sv *Service) Recipe(project, path, sum string) *Recipe {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return recipeOf(sv.projectTasks(NewChecker(), project), project, path, sum)
}

func recipeOf(tasks []*Task, project, path, sum string) *Recipe {
	byKey := map[string]*Task{}
	for _, t := range tasks {
		byKey[stepKey(t)] = t
	}
	var target *Task
	var out *Output
	for _, t := range tasks {
		for _, o := range t.Outputs {
			if o.Path == path && (sum == "" || o.SHA256 == sum) {
				target, out = t, o
			}
		}
	}
	if target == nil {
		return nil
	}
	r := &Recipe{Target: FileVersion{Path: out.Path, SHA256: out.SHA256, Class: out.Class, Now: out.Now, ProducedBy: &StepRef{target.Session, target.ID}, When: target.End}}
	need := map[string]bool{}
	seenSrc := map[string]bool{}
	readAt := map[string]int64{} // source → when the step that read it started
	var walk func(t *Task)
	walk = func(t *Task) {
		if need[stepKey(t)] {
			return
		}
		need[stepKey(t)] = true
		for _, in := range t.Inputs {
			if len(in.From) == 0 {
				if k := vkey(in.Path, in.SHA256); !seenSrc[k] {
					seenSrc[k] = true
					readAt[k] = t.Start
					r.Sources = append(r.Sources, in)
				}
				continue
			}
			for _, f := range in.From {
				s := f.Session
				if s == "" {
					s = t.Session
				}
				if up := byKey[s+"\x00"+f.Task]; up != nil {
					walk(up)
				}
			}
		}
		for _, u := range t.Uses {
			if up := byKey[t.Session+"\x00"+u.Task]; up != nil {
				walk(up)
			}
		}
	}
	walk(target)
	first := target.End
	for _, t := range tasks {
		if need[stepKey(t)] {
			r.Steps = append(r.Steps, t)
			if t.Start < first {
				first = t.Start
			}
		}
	}
	sessions := map[string]bool{}
	for _, t := range r.Steps {
		sessions[t.Session] = true
	}
	for _, t := range tasks { // the paths not taken: other steps of the same sessions, up to the target
		if !need[stepKey(t)] && sessions[t.Session] && t.End >= first && t.End <= target.End && Effect(t) {
			r.Detours++
		}
	}
	// who wrote a file before it was changed by hand: where the way continues behind the gap
	before := func(path string, until int64) *Task {
		var last *Task
		for _, t := range tasks {
			if t.End > until {
				break
			}
			for _, o := range t.Outputs {
				if o.Path == path {
					last = t
				}
			}
		}
		return last
	}
	rel := func(p string) string {
		if x, err := filepath.Rel(project, p); err == nil && !strings.HasPrefix(x, "..") {
			return x
		}
		return p
	}
	for _, src := range r.Sources {
		switch {
		case src.Status == "changed-outside":
			who := "a step"
			if w := before(src.Path, readAt[vkey(src.Path, src.SHA256)]); w != nil {
				who = fmt.Sprintf("“%s” (%s)", firstLine(w.Code()), sessionName(w))
			}
			r.Gaps = append(r.Gaps, fmt.Sprintf("%s was changed by hand after %s wrote it: that change cannot be recomputed, the path before it is broken", rel(src.Path), who))
		}
		switch src.Now {
		case NowChanged:
			r.Gaps = append(r.Gaps, fmt.Sprintf("source file %s has other content today than at the time", rel(src.Path)))
		case NowGone:
			r.Gaps = append(r.Gaps, fmt.Sprintf("source file %s no longer exists", rel(src.Path)))
		}
	}
	var stepGaps []string
	for _, t := range r.Steps {
		n := stepName(t)
		r := struct{ Gaps []string }{}
		for _, nw := range t.Network {
			if nw.Class == "remote" {
				r.Gaps = append(r.Gaps, fmt.Sprintf("%s accesses the network (%s): not a foton, needs a claim", n, nw.Address))
			}
		}
		for _, u := range t.Unrecorded {
			r.Gaps = append(r.Gaps, fmt.Sprintf("%s starts %s, which could not be recorded: what it read is missing here", n, filepath.Base(u.Program)))
		}
		if t.Code() == "" {
			r.Gaps = append(r.Gaps, fmt.Sprintf("%s: its code is not known", n))
		}
		if t.Status == "err" {
			r.Gaps = append(r.Gaps, fmt.Sprintf("%s ended with an error", n))
		}
		stepGaps = append(stepGaps, r.Gaps...)
	}
	r.Gaps = append(r.Gaps, collapse(stepGaps)...)
	r.Script = script(r, project, rel)
	r.Verdict = verdictOf(r, rel)
	r.Node = StepNodeID(target.Session, target.ID)
	return r
}

func script(r *Recipe, project string, rel func(string) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Recipe for %s (sha256 %s)\n", rel(r.Target.Path), short(r.Target.SHA256))
	fmt.Fprintf(&b, "# from %d recorded steps; %d other steps in that time were not part of it\n", len(r.Steps), r.Detours)
	b.WriteString("#\n# Source data:\n")
	for _, s := range r.Sources {
		state := map[string]string{NowSame: "unchanged", NowChanged: "DIFFERENT TODAY", NowGone: "MISSING"}[s.Now]
		fmt.Fprintf(&b, "#   %-40s sha256 %s  %s\n", rel(s.Path), short(s.SHA256), state)
	}
	if len(r.Gaps) > 0 {
		b.WriteString("#\n# Gaps (cannot be recomputed, or not recorded):\n")
		for _, g := range r.Gaps {
			fmt.Fprintf(&b, "#   - %s\n", g)
		}
	}
	fmt.Fprintf(&b, "\nsetwd(%q)\n", project)
	for i, t := range r.Steps {
		where := ""
		if o := t.Origin(); o != nil && o.File != "" {
			where = rel(o.File)
			if o.Chunk != "" {
				where += " · chunk " + o.Chunk
			}
		}
		fmt.Fprintf(&b, "\n# --- Step %d · %s", i+1, time.Unix(0, t.End).Format("15:04"))
		if where != "" {
			b.WriteString(" · " + where)
		}
		b.WriteString("\n")
		switch code := t.Code(); {
		case code == "":
			b.WriteString("# (code unknown)\n")
		case t.Origin() != nil && t.Origin().Language == "python":
			// one python3 per command: a %run is not Python, and two commands are not one -c
			for _, o := range t.Origins {
				if o.Code != "" {
					b.WriteString(pythonStep(o.Code))
				}
			}
		default:
			b.WriteString(strings.TrimRight(code, "\n") + "\n")
		}
	}
	return b.String()
}

// pythonStep: a step of a Python console in the recipe, which is an R script. A %run of a .py
// file runs that file with python3; other console code runs on its own with python3 -c,
// without the console's variables, and says so. system2 pastes its arguments into a shell line
// as they are, so each is shQuote'd: code and paths with spaces, quotes, ;, $ stay one argument.
func pythonStep(code string) string {
	rstr := func(s string) string { return strconv.Quote(s) } // Go's quoting is valid R for these strings
	if file, args, opts, ok := pyRun(code); ok && len(opts) == 0 {
		q := []string{rstr(file)}
		for _, a := range args {
			q = append(q, rstr(a))
		}
		return "stopifnot(system2(\"python3\", shQuote(c(" + strings.Join(q, ", ") + "))) == 0)  # " + strings.TrimSpace(code) + "\n"
	}
	return "# Python console code, run on its own (without the console's variables):\n" +
		"stopifnot(system2(\"python3\", shQuote(c(\"-c\", " + rstr(code) + "))) == 0)\n"
}

// Descendants is what became of a file: the steps that used it, directly or through what
// they made, and the results at the end.
type Descendants struct {
	Source  FileVersion   `json:"source"`
	Steps   []*Task       `json:"steps"`
	Results []FileVersion `json:"results"`
}

func (sv *Service) Descendants(project, path, sum string) *Descendants {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return descendantsOf(sv.projectTasks(NewChecker(), project), path, sum)
}

func descendantsOf(tasks []*Task, path, sum string) *Descendants {
	files := filesOf(tasks)
	var src *FileVersion
	for i := range files {
		if files[i].Path == path && (sum == "" || files[i].SHA256 == sum) {
			src = &files[i] // newest first: the latest matching version
			break
		}
	}
	if src == nil {
		return nil
	}
	d := &Descendants{Source: *src}
	reached := map[string]bool{vkey(src.Path, src.SHA256): true} // file versions
	in := map[string]bool{}                                      // steps
	for _, t := range tasks {                                    // time order: one pass suffices
		hit := false
		for _, i := range t.Inputs {
			if reached[vkey(i.Path, i.SHA256)] {
				hit = true
			}
		}
		for _, u := range t.Uses {
			if in[t.Session+"\x00"+u.Task] {
				hit = true
			}
		}
		if !hit {
			continue
		}
		in[stepKey(t)] = true
		d.Steps = append(d.Steps, t)
		for _, o := range t.Outputs {
			reached[vkey(o.Path, o.SHA256)] = true
		}
	}
	for _, f := range files {
		if reached[vkey(f.Path, f.SHA256)] && f.Role == "result" {
			d.Results = append(d.Results, f)
		}
	}
	return d
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:59] + "…"
	}
	return s
}

func sessionName(t *Task) string {
	if t.Script != "" {
		return filepath.Base(t.Script)
	}
	return "session " + t.Session
}

// stepName: a step as the reader can find it again, its command and the time it ended.
func stepName(t *Task) string {
	c := firstLine(t.Code())
	if shellSession(t.Session) {
		c = shellPlace(t)
	}
	if c == "" {
		c = "command " + t.ID
	}
	return fmt.Sprintf("“%s” (%s)", c, time.Unix(0, t.End).Format("15:04"))
}

// collapse: the same gap once, with how often it came up.
func collapse(gaps []string) []string {
	count := map[string]int{}
	var order []string
	for _, g := range gaps {
		if count[g] == 0 {
			order = append(order, g)
		}
		count[g]++
	}
	for i, g := range order {
		if count[g] > 1 {
			order[i] = fmt.Sprintf("%s (%d×)", g, count[g])
		}
	}
	return order
}
