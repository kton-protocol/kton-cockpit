package watch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The story of a file: everything that happened in the sessions that led to it, from their
// beginning up to the file, told step by step. Steps that contributed are on the path; the
// others are the detours, kept so the person can say what they were. Whether a step is already
// in kton is not known here (that is a question to the cockpit); the reader asks it.

// StoryStep is one thing that happened.
type StoryStep struct {
	Session  string   `json:"session"`
	Task     string   `json:"task"`
	When     int64    `json:"when_ns"`
	Where    string   `json:"where"`    // "Skript 02_nca.R", "Konsole", "Chunk #1 von step3.qmd"
	Sentence string   `json:"sentence"` // what happened, in words
	Code     string   `json:"code,omitempty"`
	Reads    []string `json:"reads,omitempty"`  // project-relative
	Writes   []string `json:"writes,omitempty"` // project-relative
	Hashes   []string `json:"hashes,omitempty"` // sha256 of the writes, for asking kton
	Uses     []string `json:"uses,omitempty"`
	Makes    []string `json:"makes,omitempty"` // variables it left
	OnPath   bool     `json:"on_path"`
	Error    bool     `json:"error,omitempty"`
	Hand     bool     `json:"hand,omitempty"` // not recorded: a file changed by hand between two steps
	Tools    []string `json:"tools,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Story is the file's whole story.
type Story struct {
	Target  FileVersion `json:"target"`
	Verdict Verdict     `json:"verdict"`
	Gaps    []string    `json:"gaps"`
	Steps   []StoryStep `json:"steps"`
	Script  string      `json:"script"`
	Repo    string      `json:"repo"`
}

func (sv *Service) Story(project, path, sum string) *Story {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return storyOf(sv.projectTasks(NewChecker(), project), project, path, sum)
}

var toolNames = []struct{ match, name string }{
	{"nmfe", "NONMEM (nmfe)"}, {"execute", "PsN execute"}, {"bbi", "bbi"}, {"quarto", "Quarto"},
	{"pharmpy", "pharmpy"}, {"summarise.py", "Python"}, {"Rscript", "Rscript"},
}

func toolsOf(t *Task) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range append(append([]Command{}, t.Commands...), t.Unrecorded...) {
		for _, tn := range toolNames {
			if strings.Contains(c.Cmdline+" "+c.Program, tn.match) && !seen[tn.name] {
				seen[tn.name] = true
				out = append(out, tn.name)
			}
		}
	}
	return out
}

func list(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "„" + x + "“"
	}
	switch len(q) {
	case 0:
		return ""
	case 1:
		return q[0]
	}
	if len(q) > 4 {
		return strings.Join(q[:3], ", ") + fmt.Sprintf(" und %d weitere", len(q)-3)
	}
	return strings.Join(q[:len(q)-1], ", ") + " und " + q[len(q)-1]
}

func storyOf(tasks []*Task, project, path, sum string) *Story {
	r := recipeOf(tasks, project, path, sum)
	if r == nil {
		return nil
	}
	rel := func(p string) string {
		if x, err := filepath.Rel(project, p); err == nil && !strings.HasPrefix(x, "..") {
			return x
		}
		return p
	}
	on := map[string]bool{}
	sessions := map[string]bool{}
	var hands []StoryStep
	// the way to the file, and behind every hand edit on it the way to the version that was
	// edited: the story starts where the first of them starts, not at the gap
	todo := [][]*Task{r.Steps}
	for len(todo) > 0 {
		steps := todo[0]
		todo = todo[1:]
		for _, t := range steps {
			if on[stepKey(t)] {
				continue
			}
			on[stepKey(t)] = true
			sessions[t.Session] = true
			for _, in := range t.Inputs {
				if in.Status != "changed-outside" {
					continue
				}
				w, sum := writerBefore(tasks, in.Path, t.Start)
				hands = append(hands, StoryStep{Session: "hand", Task: rel(in.Path) + "@" + in.SHA256[:12], When: t.Start - 1,
					Where: "außerhalb der Aufzeichnung", OnPath: true, Hand: true, Reads: []string{rel(in.Path)}, Hashes: nil,
					Sentence: "Du änderst „" + rel(in.Path) + "“ von Hand, zum Beispiel in Excel. Das wurde nicht aufgezeichnet: Was du hier geändert hast, kennt nur deine Beschreibung.",
					Warnings: []string{"nicht nachrechenbar: wer die Skripte erneut laufen lässt, bekommt die Datei ohne diese Änderung"}})
				if w != nil {
					if before := recipeOf(tasks, project, in.Path, sum); before != nil {
						todo = append(todo, before.Steps)
					}
				}
			}
		}
	}
	end := r.Target.When
	st := &Story{Target: r.Target, Verdict: r.Verdict, Gaps: r.Gaps, Script: r.Script, Repo: RepoRoot(project)}
	var picked []*Task
	for _, t := range tasks {
		if sessions[t.Session] && t.End <= end && (on[stepKey(t)] || Effect(t) || len(t.Vars) > 0 || t.Status == "err") {
			picked = append(picked, t)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].End < picked[j].End })
	for _, t := range picked {
		s := StoryStep{Session: t.Session, Task: t.ID, When: t.End, Code: t.Code(), OnPath: on[stepKey(t)], Error: t.Status == "err"}
		switch {
		case len(t.Origins) > 0 && t.Origins[0].Chunk != "":
			s.Where = "Chunk " + t.Origins[0].Chunk + " von " + rel(t.Origins[0].File)
		case t.Script != "":
			s.Where = "Skript " + rel(t.Script)
		case len(t.Origins) > 0 && t.Origins[0].File != "":
			s.Where = rel(t.Origins[0].File)
		case shellSession(t.Session):
			s.Where = "$ " + shellPlace(t)
		default:
			s.Where = "Konsole"
		}
		var srcs []string
		for _, x := range t.Sources {
			if x.Class == ClassProject {
				srcs = append(srcs, rel(x.Path))
			}
		}
		var handEdited []string
		for _, in := range t.Inputs {
			p := rel(in.Path)
			if isCodeFile(in.Path) && (contains(srcs, p) || t.Script == in.Path) {
				continue // the script itself is the "where", not something it read
			}
			s.Reads = append(s.Reads, p)
			if in.Status == "changed-outside" {
				handEdited = append(handEdited, p)
			}
		}
		for _, o := range t.Outputs {
			s.Writes = append(s.Writes, rel(o.Path))
			s.Hashes = append(s.Hashes, o.SHA256)
		}
		for _, u := range t.Uses {
			s.Uses = append(s.Uses, u.Name)
		}
		for _, v := range t.Vars {
			s.Makes = append(s.Makes, v.Name)
		}
		// the sentence: who, what was read, what ran, what came out
		var parts []string
		if len(srcs) > 0 {
			parts = append(parts, "führst "+list(srcs)+" aus")
		}
		if len(s.Reads) > 0 {
			parts = append(parts, "liest "+list(s.Reads))
		}
		if len(s.Uses) > 0 {
			parts = append(parts, "arbeitest mit "+list(s.Uses))
		}
		if tools := toolsOf(t); len(tools) > 0 {
			s.Tools = tools
			parts = append(parts, "startest "+strings.Join(tools, ", "))
		}
		if len(s.Writes) > 0 {
			parts = append(parts, "schreibst "+list(s.Writes))
		} else if len(s.Makes) > 0 {
			parts = append(parts, "legst "+list(s.Makes)+" an")
		}
		lead := "Du "
		switch {
		case strings.HasPrefix(s.Where, "Skript "):
			lead = "Im " + s.Where + " "
		case strings.HasPrefix(s.Where, "Chunk "):
			lead = "Im " + s.Where + " "
		case s.Where == "Konsole":
			lead = "In der Konsole "
		case strings.HasPrefix(s.Where, "$ "):
			lead = "Mit „" + s.Where[2:] + "“ "
		}
		if len(parts) == 0 {
			parts = []string{"führst Code aus, der keine Datei berührt"}
		}
		verb := strings.Join(parts, ", ")
		if lead == "Du " {
			s.Sentence = lead + verb + "."
		} else {
			s.Sentence = lead + strings.Replace(verb, " ", " du ", 1) + "."
		}
		if s.Error {
			s.Sentence += " Das scheitert."
		}
		for _, h := range handEdited {
			s.Warnings = append(s.Warnings, h+" war vorher von Hand geändert worden")
		}
		for _, m := range t.Missing {
			if !probe(m) {
				s.Warnings = append(s.Warnings, rel(m)+" gab es nicht")
			}
		}
		for _, n := range t.Network {
			if n.Class == "remote" {
				s.Warnings = append(s.Warnings, "Verbindung zu "+n.Address)
			}
		}
		for _, u := range t.Unrecorded {
			s.Warnings = append(s.Warnings, filepath.Base(u.Program)+" lief, konnte aber nicht aufgezeichnet werden")
		}
		st.Steps = append(st.Steps, s)
	}
	seen := map[string]bool{}
	for _, h := range hands {
		if !seen[h.Task] {
			seen[h.Task] = true
			st.Steps = append(st.Steps, h)
		}
	}
	sort.SliceStable(st.Steps, func(i, j int) bool { return st.Steps[i].When < st.Steps[j].When })
	return st
}

// writerBefore: the last step before `until` that wrote path, and the bytes it left
func writerBefore(tasks []*Task, path string, until int64) (*Task, string) {
	var last *Task
	var sum string
	for _, t := range tasks {
		if t.End > until {
			continue
		}
		for _, o := range t.Outputs {
			if o.Path == path && (last == nil || t.End >= last.End) {
				last, sum = t, o.SHA256
			}
		}
	}
	return last, sum
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Clock is the time of day of a step, for the reader.
func Clock(ns int64) string { return time.Unix(0, ns).Format("15:04") }

// shellSession: a session of shell commands, each marked from outside: Claude Code's Bash tool
// (claude-…) or a recorded shell script (shell-…, recorder/demo/beispiel.sh).
// Every command is its own place in the story, not one "Konsole" for all.
func shellSession(id string) bool {
	return strings.HasPrefix(id, "claude-") || strings.HasPrefix(id, "shell-")
}

// shellCommand: the part of a shell command line that says what it does: a leading `cd …`
// dropped, cut at the first `&&`, `;`, `|` or line break.
func shellCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	for {
		if !strings.HasPrefix(cmd, "cd ") {
			break
		}
		i := strings.IndexAny(cmd, ";&\n")
		if i < 0 {
			break
		}
		cmd = strings.TrimLeft(cmd[i:], ";& \n")
	}
	for _, sep := range []string{"\n", " && ", "; ", " | ", " # "} {
		if i := strings.Index(cmd, sep); i > 0 {
			cmd = cmd[:i]
		}
	}
	cmd = strings.TrimSpace(cmd)
	if r := []rune(cmd); len(r) > 60 {
		cmd = string(r[:59]) + "…"
	}
	return cmd
}

// The tools a shell command started, as the reader names them: the program and its arguments,
// paths shortened to their file name. Scripts run by an interpreter (bash nmfe75, perl vpc)
// are named by the script.
var toolProg = regexp.MustCompile(`^(Rscript|R|nmfe\d*|execute|vpc|bootstrap|scm|sse|psn|bbi|quarto|python3?(\.\d+)?)$`)
var scriptRunners = map[string]bool{"sh": true, "bash": true, "dash": true, "perl": true, "env": true}

func toolCall(cmdline string) string {
	f := splitCmdline(cmdline)
	for len(f) > 1 && scriptRunners[filepath.Base(f[0])] && !strings.HasPrefix(f[1], "-") {
		f = f[1:]
	}
	if len(f) == 0 || !toolProg.MatchString(filepath.Base(f[0])) {
		return ""
	}
	if b := filepath.Base(f[0]); b == "R" && !strings.Contains(cmdline, "--file=") && !contains(f, "-f") {
		return "" // an R that is not running a script: a probe or a helper
	}
	out := make([]string, len(f))
	for i, a := range f {
		if strings.HasPrefix(a, "/") {
			a = filepath.Base(a)
		}
		out[i] = a
	}
	c := strings.Join(out, " ")
	if r := []rune(c); len(r) > 60 {
		c = string(r[:59]) + "…"
	}
	return c
}

// shellPlace: what a shell command did, by the outermost tools it started (`Rscript R/01.R ·
// Rscript R/02.R`, `nmfe75 run001.mod run001.lst`); a tool started by another tool (NONMEM
// under PsN, R under Rscript) is part of it. Without any tool: the command line itself.
func shellPlace(t *Task) string {
	parent := map[int]int{}
	for p, pp := range t.Parents {
		parent[p] = pp
	}
	for _, c := range t.Commands {
		if _, ok := parent[c.PID]; !ok {
			parent[c.PID] = c.PPID
		}
	}
	tool := map[int]string{}
	var order []int
	for _, c := range t.Commands {
		if _, seen := tool[c.PID]; seen {
			continue
		}
		if n := toolCall(c.Cmdline); n != "" {
			tool[c.PID] = n
			order = append(order, c.PID)
		}
	}
	var names []string
	for _, pid := range order {
		nested := false
		for p, hops := parent[pid], 0; p != 0 && hops < 64; p, hops = parent[p], hops+1 {
			if _, ok := tool[p]; ok {
				nested = true
				break
			}
		}
		if !nested && !contains(names, tool[pid]) {
			names = append(names, tool[pid])
		}
	}
	switch {
	case len(names) == 0:
		return shellCommand(t.Code())
	case len(names) > 3:
		return strings.Join(names[:3], " · ") + fmt.Sprintf(" · +%d", len(names)-3)
	}
	return strings.Join(names, " · ")
}
