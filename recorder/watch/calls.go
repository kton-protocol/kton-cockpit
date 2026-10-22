package watch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Calls: a step cut at the tools it started. A tool call (nmfe75, PsN's execute or vpc, bbi,
// python, Rscript, quarto) is a function: the files it read, the command line, and the files it
// left. Calls nest: NONMEM under PsN is a call of its own inside the PsN call.
//
// Key is the call's identity in the shape of kton's action key (SPEC §6.3): the inputs as
// {path relative to the working directory → hash} and the command as protocol. It is local
// until kton decides how the environment enters the key (ADR-010): the environment is meant to
// enter as its class, so nothing about the concrete installation is in here. Files outside the
// project that are not environment (a licence, a dataset on a share) are listed, not keyed, and
// so are project files the call looked for and did not find. Neither may have changed when a
// call is reused (ReuseBlocked).

// CallFile is one file of a call.
type CallFile struct {
	Path   string `json:"path"` // absolute
	Rel    string `json:"rel"`  // relative to the call's working directory
	SHA256 string `json:"sha256"`
}

// Call is one tool call.
type Call struct {
	Session string     `json:"session,omitempty"`
	Task    string     `json:"task"`
	PID     int        `json:"pid"`
	Parent  int        `json:"parent,omitempty"` // pid of the enclosing call
	Tool    string     `json:"tool"`             // what the reader sees: nmfe75 run001.mod run001.lst
	Argv    []string   `json:"argv"`             // normalised: program name, paths relative to cwd
	Cwd     string     `json:"cwd"`
	Reads   []CallFile `json:"reads"`
	Writes  []CallFile `json:"writes"`
	Outside []CallFile `json:"outside,omitempty"` // read outside the project, not keyed
	Missing []CallFile `json:"missing,omitempty"` // looked for in the project, not there; not keyed
	Key     string     `json:"key"`
}

// CallsOf returns the tool calls of a task, outer calls before the ones they contain.
func CallsOf(t *Task) []Call {
	parent := map[int]int{}
	for p, pp := range t.Parents {
		parent[p] = pp
	}
	first := map[int]Command{}
	var order []int
	for _, c := range t.Commands {
		if _, ok := parent[c.PID]; !ok {
			parent[c.PID] = c.PPID
		}
		if _, ok := first[c.PID]; ok {
			continue
		}
		if toolCall(c.Cmdline) != "" {
			first[c.PID] = c
			order = append(order, c.PID)
		}
	}
	enclosing := func(pid int) int { // nearest tool call above pid
		for p, hops := parent[pid], 0; p != 0 && hops < 256; p, hops = parent[p], hops+1 {
			if _, ok := first[p]; ok {
				return p
			}
		}
		return 0
	}
	// R under Rscript is Rscript's own child, not a second call
	for _, pid := range order {
		c := first[pid]
		if up := enclosing(pid); up != 0 && toolName(c.Cmdline) == "R" && toolName(first[up].Cmdline) == "Rscript" {
			delete(first, pid)
		}
	}
	within := func(pid, root int) bool {
		for p, hops := pid, 0; p != 0 && hops < 256; p, hops = parent[p], hops+1 {
			if p == root {
				return true
			}
		}
		return false
	}
	var calls []Call
	for _, pid := range order {
		c, ok := first[pid]
		if !ok {
			continue
		}
		call := Call{Session: t.Session, Task: t.ID, PID: pid, Parent: enclosing(pid), Tool: toolCall(c.Cmdline), Cwd: c.Cwd,
			Argv: normArgv(c.Cmdline, c.Cwd)}
		written := map[string]bool{}
		opened := map[string]bool{} // opened for writing inside the call: not a stdout it inherited
		reads, writes, outside := map[string]string{}, map[string]string{}, map[string]string{}
		missing := map[string]bool{}
		var readOrder, writeOrder, outsideOrder, missingOrder []string
		for _, o := range t.Ops {
			if !within(o.PID, pid) || Scratch(o.Path) || strings.HasSuffix(o.Path, " (deleted)") {
				continue // the tool's own work files
			}
			switch o.Op {
			case "r":
				if written[o.Path] {
					continue // its own intermediate
				}
				m, ord := reads, &readOrder
				if o.Outside {
					m, ord = outside, &outsideOrder
				}
				if _, seen := m[o.Path]; !seen {
					m[o.Path] = o.SHA256
					*ord = append(*ord, o.Path)
				}
			case "w":
				written[o.Path] = true
				opened[o.Path] = true
			case "c":
				written[o.Path] = true
				if o.Outside || !opened[o.Path] {
					continue // written through a descriptor the caller handed down (its redirect)
				}
				if _, seen := writes[o.Path]; !seen {
					writeOrder = append(writeOrder, o.Path)
				}
				writes[o.Path] = o.SHA256
			case "m":
				if !written[o.Path] && !missing[o.Path] {
					missing[o.Path] = true
					missingOrder = append(missingOrder, o.Path)
				}
			case "d":
				// removed within the call: not what it leaves. An inner call keeps it as its
				// output when the outer one removes it afterwards (PsN cleans up NONMEM's files).
				delete(writes, o.Path)
			}
		}
		file := func(p, h string) CallFile { return CallFile{Path: p, Rel: relTo(c.Cwd, p), SHA256: h} }
		for _, p := range readOrder {
			call.Reads = append(call.Reads, file(p, reads[p]))
		}
		for _, p := range writeOrder {
			if _, left := writes[p]; !left {
				continue
			}
			if h, read := reads[p]; read && h == writes[p] {
				continue // opened for writing, left as it was (NONMEM opens its data read-write)
			}
			call.Writes = append(call.Writes, file(p, writes[p]))
		}
		for _, p := range outsideOrder {
			call.Outside = append(call.Outside, file(p, outside[p]))
		}
		for _, p := range missingOrder {
			if _, read := reads[p]; !read && !written[p] { // it created it itself, or found it later
				call.Missing = append(call.Missing, file(p, ""))
			}
		}
		call.Key = callKey(call)
		calls = append(calls, call)
	}
	return calls
}

// toolName: the program a tool call runs (nmfe75, vpc, Rscript), or "".
func toolName(cmdline string) string {
	f := strings.Fields(toolCall(cmdline))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func relTo(dir, p string) string {
	if dir == "" {
		return p
	}
	if r, err := filepath.Rel(dir, p); err == nil {
		return r
	}
	return p
}

// NormArgv is normArgv for a command line given as its arguments (a stub's own os.Args).
func NormArgv(args []string, cwd string) []string { return normArgv(shellJoin(args), cwd) }

// splitCmdline splits a command line as the shim writes it: arguments separated by spaces, an
// argument with other characters in single quotes ('run 1.mod', 'it'\”s').
func splitCmdline(s string) []string {
	var out []string
	var b strings.Builder
	in, quoted := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			b.WriteByte(c)
		case c == '\'':
			quoted, in = true, true
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		case c == '\\' && i+1 < len(s) && s[i+1] == '\'':
			b.WriteByte('\'') // the \' between two quoted parts of 'it'\''s'
			i++
			in = true
		default:
			b.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, b.String())
	}
	return out
}

// normArgv: the command line as the key sees it. The program by name; an absolute path inside
// the repository (RepoRoot of the working directory) relative to the working directory, with
// ".." where it lies above; any other absolute path (an installation, a licence) by its file
// name, since it belongs to the environment. Also inside key=value. The recorder and kton-call
// both come here, so a call and its stub see the same command line.
func normArgv(cmdline, cwd string) []string {
	f := splitCmdline(cmdline)
	for len(f) > 1 && scriptRunners[filepath.Base(f[0])] && !strings.HasPrefix(f[1], "-") {
		f = f[1:]
	}
	out := make([]string, len(f))
	root := cwd
	if cwd != "" {
		root = RepoRoot(cwd)
	}
	norm := func(p string) string {
		if !strings.HasPrefix(p, "/") {
			return p
		}
		if cwd == "" {
			return filepath.Base(p)
		}
		if r, err := filepath.Rel(root, p); err != nil || r == ".." || strings.HasPrefix(r, "../") {
			return filepath.Base(p) // outside the repository: the environment
		}
		if r, err := filepath.Rel(cwd, p); err == nil {
			return r
		}
		return filepath.Base(p)
	}
	for i, a := range f {
		switch {
		case i == 0:
			a = filepath.Base(a)
		case strings.Contains(a, "=/"):
			k := a[:strings.Index(a, "=")+1]
			a = k + norm(a[len(k):])
		default:
			a = norm(a)
		}
		out[i] = a
	}
	return out
}

// callKey: sha256 of {inputs: {rel → hash}, protocol: {kind: "cmd", ref: sha256({argv})}}, both
// serialised with sorted keys.
func callKey(c Call) string {
	inputs := map[string]string{}
	for _, r := range c.Reads {
		inputs[r.Rel] = r.SHA256
	}
	return CallKey(c.Argv, inputs)
}

// ReuseBlocked says why a recorded call must not be reused in cwd although its key matches,
// or "" when nothing speaks against it: a file read outside the project that was not hashed or
// has other bytes now (or is gone), or a project file the call looked for that exists now.
// hash returns a file's sha256 in hex, "" when it cannot be read.
func ReuseBlocked(c Call, cwd string, hash func(path string) string) string {
	for _, o := range c.Outside {
		want := strings.TrimPrefix(o.SHA256, "sha256:")
		if want == "" {
			return o.Path + " (outside the project) was read but not hashed"
		}
		if got := hash(o.Path); got != want {
			return o.Path + " (outside the project) is not the file the call read"
		}
	}
	for _, m := range c.Missing {
		if _, err := os.Stat(filepath.Join(cwd, m.Rel)); err == nil {
			return m.Rel + " was not there when the call ran, and is now"
		}
	}
	return ""
}

// CallKey is the key of a call with this normalised command line and these inputs
// ({path relative to the working directory → hash}).
func CallKey(argv []string, inputs map[string]string) string {
	ref := sha256.Sum256(canonJSON(map[string]any{"argv": argv}))
	return "sha256:" + hex.EncodeToString(sha256Of(canonJSON(map[string]any{
		"inputs":   inputs,
		"protocol": map[string]any{"kind": "cmd", "ref": "sha256:" + hex.EncodeToString(ref[:])},
	})))
}

func sha256Of(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

func canonJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // maps encode with sorted keys
	return bytes.TrimRight(b.Bytes(), "\n")
}

// callsOf: CallsOf, kept per task until the task grows (its ops or its end change).
func (sv *Service) callsOf(session string, t *Task) []Call {
	k := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", session, t.ID, len(t.Ops), len(t.Commands), t.End)
	if v, ok := sv.callMemo.Load(k); ok {
		return v.([]Call)
	}
	tt := *t
	tt.Session = session
	cs := CallsOf(&tt)
	sv.callMemo.Store(k, cs)
	return cs
}

// ProjectCalls: every tool call of a project, in time order.
func (sv *Service) ProjectCalls(project string) []Call {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	var out []Call
	for _, t := range sv.projectTasks(NewChecker(), project) {
		out = append(out, sv.callsOf(t.Session, t)...)
	}
	return out
}

// CallsByKey: every recorded call with this key, in any project.
func (sv *Service) CallsByKey(key string) []Call {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	var out []Call
	for _, tr := range sv.sessions {
		for _, t := range tr.s.Tasks {
			for _, c := range sv.callsOf(tr.s.ID, t) {
				if c.Key == key {
					out = append(out, c)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

// CallsByArgv: every recorded call with this normalised command line, newest first. A stub
// asks this before it runs: the earlier calls say which files such a call reads.
func (sv *Service) CallsByArgv(argv []string) []Call {
	want := strings.Join(argv, "\x00")
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	type found struct {
		c   Call
		end int64
	}
	var all []found
	for _, tr := range sv.sessions {
		for _, t := range tr.s.Tasks {
			for _, c := range sv.callsOf(tr.s.ID, t) {
				if strings.Join(c.Argv, "\x00") == want {
					all = append(all, found{c, t.End})
				}
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].end > all[j].end })
	out := make([]Call, len(all))
	for i, f := range all {
		out[i] = f.c
	}
	return out
}
