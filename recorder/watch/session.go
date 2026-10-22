package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Session is one event log: one R session, one Positron kernel, one Claude session.
type Session struct {
	ID          string  `json:"id"`
	Log         string  `json:"log"`
	Project     string  `json:"project"`
	Start       int64   `json:"start_ns"`
	Last        int64   `json:"last_ns"`
	Closed      bool    `json:"closed"`
	CloseReason string  `json:"close_reason,omitempty"` // end | timeout | eof
	Script      string  `json:"script,omitempty"`       // the session is `R -f`/`Rscript` of this file
	ScriptSHA   string  `json:"script_sha256,omitempty"`
	Tasks       []*Task `json:"tasks"`

	cls        *Classifier
	cur        *taskState
	seq        int
	owner      int              // pid whose markers count; others are an R a tool started underneath
	console    map[string]bool  // files the session's stdout/stderr go to (`R -f x.R > log`)
	redirects  map[int][]string // before any marker: write fds a process inherited from the session's own redirect
	producedBy map[string][]Ref // content hash → outputs that had it
	lastOutput map[string]*Output
	seenFiles  []FileOp          // project file versions seen since the service last took them (for the CAS)
	known      map[string]string // path → last content hash seen (closew, outputs)
	procs      map[int]bool
	hashCache  map[string]string
	escaped    bool // the log's header says v2 or later: path and extra are escaped
	quoted     bool // v3: command lines quote their arguments; older ones are read as split by spaces

	// Hash computes a file's content hash when the shim did not (files outside the hashed
	// prefixes). Replaceable in tests.
	Hash func(path string) (sum string, status string) `json:"-"`
}

type taskState struct {
	t          *Task
	written    map[string]bool
	closeSum   map[string]string
	closeAt    map[string][2]int64
	deleted    map[string]bool
	movedAway  map[string]bool
	firstRead  map[string]string
	readMtime  map[string]int64 // mtime when first read: a write changes it even if the bytes do not
	recreated  map[string]bool  // deleted or created anew in this task (a scratch file of the tool)
	inputIdx   map[string]bool
	listIdx    map[string]bool
	missingIdx map[string]bool
	sourceIdx  map[string]bool
	procs      map[int]bool
	spawns     map[int]Command // started, waiting for the program's own exec line
	opIdx      map[FileOp]bool
}

// op notes a file operation of a process on a project file once.
func (c *taskState) op(o FileOp) {
	if !c.opIdx[o] {
		c.opIdx[o] = true
		c.t.Ops = append(c.t.Ops, o)
	}
}

// seen hands a file version to the CAS right when the log shows it, not when the command ends:
// a tool may delete its files before (PsN zips its run directories at the end).
func (s *Session) seen(o FileOp) {
	if o.SHA256 != "" && !o.Outside && !Scratch(o.Path) {
		s.seenFiles = append(s.seenFiles, o)
	}
}

// TakeSeen returns the file versions seen since the last call, and forgets them.
func (s *Session) TakeSeen() []FileOp {
	out := s.seenFiles
	s.seenFiles = nil
	return out
}

func newTaskState(start int64) *taskState {
	return &taskState{
		t:       &Task{Start: start},
		written: map[string]bool{}, closeSum: map[string]string{}, closeAt: map[string][2]int64{},
		deleted: map[string]bool{}, movedAway: map[string]bool{}, firstRead: map[string]string{}, readMtime: map[string]int64{}, recreated: map[string]bool{},
		inputIdx: map[string]bool{}, listIdx: map[string]bool{}, missingIdx: map[string]bool{},
		sourceIdx: map[string]bool{}, procs: map[int]bool{}, spawns: map[int]Command{}, opIdx: map[FileOp]bool{},
	}
}

// NewSession starts an empty session. The classifier's Project may be left empty; the log
// header fills it.
func NewSession(id, log string, cls *Classifier) *Session {
	s := &Session{
		ID: id, Log: log, cls: cls,
		producedBy: map[string][]Ref{}, lastOutput: map[string]*Output{}, known: map[string]string{},
		procs: map[int]bool{}, hashCache: map[string]string{}, console: map[string]bool{}, redirects: map[int][]string{},
	}
	s.Hash = s.hashFile
	s.Project = cls.Project
	s.cur = newTaskState(0)
	return s
}

// Classifier exposes the session's path rules (read-only use).
func (s *Session) Classifier() *Classifier { return s.cls }

// PIDs are all processes the session has seen.
func (s *Session) PIDs() []int {
	var out []int
	for p := range s.procs {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func (s *Session) hashFile(p string) (string, string) {
	st, err := os.Stat(p)
	if err != nil {
		return "", "gone"
	}
	key := fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
	if h, ok := s.hashCache[key]; ok {
		return h, "hashed-by-watcher"
	}
	f, err := os.Open(p)
	if err != nil {
		return "", "gone"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", "gone"
	}
	sum := hex.EncodeToString(h.Sum(nil))
	s.hashCache[key] = sum
	return sum, "hashed-by-watcher"
}

// Line feeds one raw log line (header or event).
func (s *Session) Line(line string) {
	if h, ok := ParseHeader(line); ok {
		s.escaped, s.quoted = h.Escaped(), h.Quoted()
		if len(h.Hash) > 0 && s.cls.Project == "" {
			s.cls.Project = h.Hash[0]
			s.Project = h.Hash[0]
		}
		return
	}
	parse := Parse
	if s.escaped {
		parse = ParseEscaped
	}
	if e, ok := parse(line); ok {
		s.Handle(e)
	}
}

// Handle applies one event.
func (s *Session) Handle(e Event) {
	if s.Closed {
		return
	}
	if s.Start == 0 {
		s.Start = e.TS
		s.cur.t.Start = e.TS
	}
	if e.TS > s.Last {
		s.Last = e.TS
	}
	c := s.cur
	t := c.t
	switch e.Op {
	case "TASK", "END", "SOURCE", "ENV", "VARS":
		if s.owner == 0 {
			s.owner = e.PID
			// a traced wrapper (`sh run.sh` with `R -f x.R > log` in it): the redirect the
			// marking R received is its console, not an output of its first command
			for _, p := range s.redirects[e.PID] {
				s.console[p] = true
			}
		} else if e.PID != s.owner && s.procs[e.PID] {
			// markers of a nested R session (one a tool started underneath): that R is itself a
			// traced process. A marker from a process the shim never saw is the session's own,
			// written from outside, like the Claude Code hook that marks each Bash command from a
			// new, untraced process.
			return
		}
	default:
		c.procs[e.PID] = true
		s.procs[e.PID] = true
		if e.PPID != 0 {
			if t.Parents == nil {
				t.Parents = map[int]int{}
			}
			if _, ok := t.Parents[e.PID]; !ok {
				t.Parents[e.PID] = e.PPID
			}
		}
	}
	switch e.Op {
	case "exec":
		cmdline := s.cmdline(e.Extra)
		prog := ""
		if f := splitCmdline(cmdline); len(f) > 0 {
			prog = f[0]
		}
		t.Commands = append(t.Commands, Command{PID: e.PID, PPID: e.PPID, Program: prog, Cmdline: cmdline, Cwd: e.Path})
		delete(c.spawns, e.PID)
		if s.Script == "" && s.owner == 0 && s.seq == 0 {
			s.Script = rScriptArg(cmdline, e.Path)
		}
	case "spawn":
		pid := e.PID
		if ch := e.KV("child"); ch != "" {
			fmt.Sscan(ch, &pid)
		}
		argv := e.Extra
		if i := strings.Index(argv, "argv="); i >= 0 {
			argv = argv[i+5:]
		}
		c.spawns[pid] = Command{PID: pid, PPID: e.PID, Program: e.Path, Cmdline: s.cmdline(argv)}
	case "connect":
		s.connect(t, e)
	case "open", "inherit":
		s.open(c, e)
	case "closew":
		if cl := s.cls.Class(e.Path); cl == ClassProject || cl == ClassOutside {
			o := FileOp{PID: e.PID, Op: "c", Path: e.Path, SHA256: e.KV("sha256"), Outside: cl == ClassOutside}
			c.op(o)
			s.seen(o)
		}
		c.closeSum[e.Path] = e.KV("sha256")
		c.closeAt[e.Path] = [2]int64{e.Size, e.Mtime}
		if h := e.KV("sha256"); h != "" {
			s.known[e.Path] = h
		}
	case "rename":
		src, dst := e.Extra, e.Path
		if cl := s.cls.Class(src); cl == ClassProject {
			c.op(FileOp{PID: e.PID, Op: "d", Path: src}) // gone from where it was
		}
		if c.written[src] {
			delete(c.written, src)
			c.movedAway[src] = true
		}
		if h, ok := c.closeSum[src]; ok {
			c.closeSum[dst] = h
			c.closeAt[dst] = c.closeAt[src]
			delete(c.closeSum, src)
			delete(c.closeAt, src)
		} else if h, ok := s.known[src]; ok {
			c.closeSum[dst] = h
		}
		if h, ok := s.known[src]; ok {
			s.known[dst] = h
			delete(s.known, src)
		}
		c.written[dst] = true
		delete(c.deleted, dst)
		if h := c.closeSum[dst]; h != "" && s.cls.Class(dst) == ClassProject {
			o := FileOp{PID: e.PID, Op: "c", Path: dst, SHA256: h}
			c.op(o)
			s.seen(o)
		}
	case "unlink":
		if s.cls.Class(e.Path) == ClassProject {
			c.op(FileOp{PID: e.PID, Op: "d", Path: e.Path})
		}
		if c.written[e.Path] {
			c.deleted[e.Path] = true
		}
		c.recreated[e.Path] = true
		delete(s.known, e.Path)
	case "openfail":
		if s.cls.Class(e.Path) == ClassProject {
			c.op(FileOp{PID: e.PID, Op: "m", Path: e.Path}) // per process, for the calls
			if !c.missingIdx[e.Path] {
				c.missingIdx[e.Path] = true
				t.Missing = append(t.Missing, e.Path)
			}
		}
	case "SOURCE":
		if !c.sourceIdx[e.Path] {
			c.sourceIdx[e.Path] = true
			t.Sources = append(t.Sources, Source{Path: e.Path, Class: s.cls.Class(e.Path)})
		}
	case "ENV":
		s.cls.AddEnv(strings.Split(e.Path, ",")...)
		if p := e.KV("project"); p != "" {
			// the session says where it works; more specific than the broad hashed prefix
			s.cls.Project, s.Project = p, p
		}
		// what the runtime read before it could say where it lives was its environment too
		kept := t.Inputs[:0]
		for _, in := range t.Inputs {
			if in.Class = s.cls.Class(in.Path); in.Class == ClassEnv {
				t.EnvFiles++
				continue
			}
			kept = append(kept, in)
		}
		t.Inputs = kept
	case "VARS":
		for _, kv := range strings.Split(e.Extra, ";") {
			if n, d, ok := strings.Cut(kv, "="); ok && n != "" {
				t.Vars = append(t.Vars, Var{Name: n, Desc: d})
			}
		}
	case "TASK":
		t.ID, t.Status, t.Expr, t.End = e.Path, e.Mode, e.Extra, e.TS
		s.finish()
		s.cur = newTaskState(e.TS)
	case "END":
		s.Close("end", e.TS)
	}
}

func (s *Session) connect(t *Task, e Event) {
	class := e.KV("class")
	if class == "" { // logs from before the shim classified
		switch {
		case strings.HasPrefix(e.Path, "unix:"):
			class = "unix"
		case strings.HasPrefix(e.Path, "inet:127."), strings.HasPrefix(e.Path, "inet6:[::1]"):
			class = "loopback"
		default:
			class = "remote"
		}
	}
	rc, errno := e.KV("rc"), e.KV("errno")
	if rc == "" { // old format "rc=-1 errno=115"
		fmt.Sscanf(e.Extra, "rc=%s errno=%s", &rc, &errno)
	}
	t.Network = append(t.Network, Net{PID: e.PID, Address: e.Path, Class: class, OK: rc == "0" || errno == "115"})
}

func (s *Session) open(c *taskState, e Event) {
	t := c.t
	class := s.cls.Class(e.Path)
	switch class {
	case ClassSelf, ClassSystem:
		return
	case ClassEnv:
		t.EnvFiles++
		return
	}
	if strings.HasSuffix(e.Mode, "d") {
		if class != ClassTemp && !c.listIdx[e.Path] {
			c.listIdx[e.Path] = true
			t.Listings = append(t.Listings, e.Path)
		}
		return
	}
	mode := strings.TrimSuffix(e.Mode, "d")
	writes := strings.Contains(mode, "w")
	if e.Op == "inherit" && s.owner == 0 && s.seq == 0 {
		if !c.written[e.Path] {
			s.console[e.Path] = true // the session's own console log, before any command
			return
		}
		// a file the session opened itself and handed to a child (`awk … > out.csv` in a
		// script) is an output, unless that child turns out to be the marking R (see above)
		s.redirects[e.PID] = append(s.redirects[e.PID], e.Path)
	}
	if e.Op == "inherit" || class == ClassTemp {
		if writes {
			c.written[e.Path] = true
		}
		return
	}
	fresh := e.KV("new") == "1" || e.KV("trunc") == "1"
	if class == ClassProject || class == ClassOutside {
		if strings.HasPrefix(mode, "r") && !fresh {
			o := FileOp{PID: e.PID, Op: "r", Path: e.Path, SHA256: e.KV("sha256"), Outside: class == ClassOutside}
			if o.Outside && o.SHA256 == "" && e.KV("unhashed") == "" && stillAsRead(e) {
				o.SHA256, _ = s.Hash(e.Path) // the shim hashes the project only; a reuse checks this one too
			}
			c.op(o)
			s.seen(o)
		}
		if writes {
			c.op(FileOp{PID: e.PID, Op: "w", Path: e.Path, Outside: class == ClassOutside})
		}
	}
	if s.Script != "" && e.Path == s.Script && s.ScriptSHA == "" {
		s.ScriptSHA = e.KV("sha256")
	}
	if strings.HasPrefix(mode, "r") && !fresh && !c.written[e.Path] {
		h, st := e.KV("sha256"), "observed"
		switch {
		case e.KV("unhashed") == "large":
			st = "unhashed-large"
		case h == "":
			h, st = s.Hash(e.Path)
		}
		key := e.Path + "\x00" + h
		if !c.inputIdx[key] {
			c.inputIdx[key] = true
			in := &Input{Path: e.Path, Class: class, SHA256: h, Status: st, PID: e.PID}
			s.link(in)
			t.Inputs = append(t.Inputs, in)
		}
		if _, ok := c.firstRead[e.Path]; !ok {
			c.firstRead[e.Path] = h
			c.readMtime[e.Path] = e.Mtime
		}
	}
	if writes {
		c.written[e.Path] = true
		if fresh {
			c.recreated[e.Path] = true
		}
	}
}

// racyNS: a file modified this shortly before it was read may have been rewritten with the
// same size inside one mtime tick (the shim's rule for its hash cache, too).
const racyNS = 2_000_000_000

// stillAsRead: the file as it is now is the one the event read, as far as size and mtime tell,
// and its mtime was not racy at the read. The watcher may process a line long after it was
// written (catch-up, restart); hashing today's bytes then would name the wrong input.
func stillAsRead(e Event) bool {
	st, err := os.Stat(e.Path)
	return err == nil && e.Mtime != 0 && st.Size() == e.Size && st.ModTime().UnixNano() == e.Mtime && e.TS-e.Mtime >= racyNS
}

// link connects an input to the earlier output it came from. Only the same path counts as
// provenance: a different file with the same bytes (pharmpy's dataset copy, two identical CSVs)
// is a coincidence of content, recorded but not followed.
func (s *Session) link(in *Input) {
	if in.SHA256 == "" || in.SHA256 == EmptySHA256 {
		return
	}
	if prev, ok := s.lastOutput[in.Path]; ok {
		if prev.SHA256 == in.SHA256 {
			for i := len(s.producedBy[in.SHA256]) - 1; i >= 0; i-- {
				if r := s.producedBy[in.SHA256][i]; r.Path == in.Path {
					in.From = []Ref{r}
					return
				}
			}
		} else {
			in.Status = "changed-outside" // written by a task, then changed by nothing that was recorded
			return
		}
	}
	seen := map[string]bool{}
	for _, r := range s.producedBy[in.SHA256] {
		if !seen[r.Path] && r.Path != in.Path {
			seen[r.Path] = true
			in.SameAs = append(in.SameAs, r)
		}
	}
}

func (s *Session) finish() {
	c := s.cur
	t := c.t
	s.seq++
	t.Seq = s.seq
	// a file the command looked for and then created itself was not missing
	stillMissing := t.Missing[:0]
	for _, m := range t.Missing {
		if !c.written[m] {
			stillMissing = append(stillMissing, m)
		}
	}
	t.Missing = stillMissing
	paths := make([]string, 0, len(c.written))
	for p := range c.written {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if s.console[p] {
			continue
		}
		if c.deleted[p] || s.cls.Class(p) == ClassTemp {
			t.Temporary = append(t.Temporary, p)
			continue
		}
		h, ok := c.closeSum[p]
		st := "observed"
		if ok && h == "" {
			st = "unhashed-large"
		}
		if !ok {
			// no closew: written through something the shim did not see close. Ask the file.
			if h, st = s.Hash(p); st == "gone" {
				t.Temporary = append(t.Temporary, p)
				continue
			}
		}
		// ... and a process the shim cannot see (a static binary: pandoc embedding resources)
		// may have written it again after the last write the shim saw. When the file changed
		// after that write but before the command ended, it is the command's output; a change
		// after the end (a hand edit) is not.
		if at, seen := c.closeAt[p]; ok && h != "" && seen && t.End > 0 {
			if mt, okm := fileMtime(p); okm && mt != at[1] && mt <= t.End {
				if dh, dst := s.Hash(p); dh != "" && dh != h && dst != "gone" {
					h, st = dh, "written-unseen"
					o := FileOp{PID: lastWriter(t, p), Op: "c", Path: p, SHA256: h}
					c.op(o)
					s.seen(o)
				}
			}
		}
		// A closew hash is never replaced by what is on disk now: the watcher may read the log
		// after someone edited the file by hand, and that edit is not this task's output.
		if fr, read := c.firstRead[p]; read && fr == h && !c.recreated[p] && !c.rewrote(p) {
			continue // opened for writing, left unchanged: not an output
		}
		out := &Output{Path: p, Class: s.cls.Class(p), SHA256: h, Status: st}
		if prev, ok := s.lastOutput[p]; ok && prev.SHA256 != h {
			prev.Overwritten = true
		}
		s.lastOutput[p] = out
		if h != "" && h != EmptySHA256 {
			s.producedBy[h] = append(s.producedBy[h], Ref{Task: t.ID, Path: p})
		}
		s.known[p] = h
		t.Outputs = append(t.Outputs, out)
	}
	// programs and scripts the task started are environment, not data (bash reads nmfe75)
	progs := map[string]bool{}
	for _, cm := range t.Commands {
		for _, f := range splitCmdline(cm.Cmdline) {
			if strings.HasPrefix(f, "/") {
				progs[f] = true
				if r, err := filepath.EvalSymlinks(f); err == nil {
					progs[r] = true
				}
			}
		}
	}
	kept := t.Inputs[:0]
	for _, in := range t.Inputs {
		if progs[in.Path] && in.Class != ClassProject {
			t.EnvFiles++
			continue
		}
		if c.written[in.Path] {
			if h, ok := c.closeSum[in.Path]; !ok || h != in.SHA256 || c.recreated[in.Path] || c.rewrote(in.Path) {
				in.Rewritten = true
			}
		}
		kept = append(kept, in)
	}
	t.Inputs = kept
	for p := range c.movedAway {
		if !c.written[p] {
			t.Temporary = append(t.Temporary, p) // written, then renamed into its final place
		}
	}
	sort.Strings(t.Temporary)
	for _, sp := range c.spawns {
		t.Unrecorded = append(t.Unrecorded, sp)
	}
	sort.Slice(t.Unrecorded, func(i, j int) bool { return t.Unrecorded[i].PID < t.Unrecorded[j].PID })
	for p := range c.procs {
		t.PIDs = append(t.PIDs, p)
	}
	t.Script, t.ScriptSHA = s.Script, s.ScriptSHA
	sort.Ints(t.PIDs)
	if t.ID == "tail" && len(t.Inputs)+len(t.Outputs)+len(t.Commands)+len(t.Network)+len(t.Missing) == 0 {
		return
	}
	s.Tasks = append(s.Tasks, t)
}

// Close ends the session: what came after the last marker becomes the task "tail".
func (s *Session) Close(reason string, ts int64) {
	if s.Closed {
		return
	}
	t := s.cur.t
	t.ID, t.Status, t.Expr, t.End = "tail", "-", "(nach dem letzten Befehl)", ts
	s.finish()
	s.Closed = true
	s.CloseReason = reason
}

// rewrote: the file's mtime at its last tracked close differs from when it was first read, so
// the task wrote it, even if the bytes came out the same (a NONMEM rerun of the same model).
func (c *taskState) rewrote(p string) bool {
	at, ok := c.closeAt[p]
	rm, read := c.readMtime[p]
	return ok && read && at[1] != 0 && rm != 0 && at[1] != rm
}

// cmdline puts a logged command line in v3's quoted form: a line from an older shim was joined
// by spaces, so its words are its arguments, and a quote in one is just a character.
func (s *Session) cmdline(line string) string {
	if s.quoted {
		return line
	}
	return shellJoin(strings.Fields(line))
}

// rScriptArg: the file of `R -f x.R`, `R --file=x.R`, `Rscript x.R`, made absolute against cwd.
func rScriptArg(cmdline, cwd string) string {
	f := splitCmdline(cmdline)
	if len(f) == 0 {
		return ""
	}
	prog := filepath.Base(f[0])
	if prog != "R" && prog != "Rscript" {
		return ""
	}
	path := ""
	for i := 1; i < len(f); i++ {
		switch {
		case f[i] == "-f" && i+1 < len(f):
			path = f[i+1]
		case strings.HasPrefix(f[i], "--file="):
			path = strings.TrimPrefix(f[i], "--file=")
		case prog == "Rscript" && !strings.HasPrefix(f[i], "-") && path == "":
			path = f[i]
		}
	}
	if path == "" || !strings.HasSuffix(strings.ToLower(path), ".r") {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// fileMtime: a file's modification time in ns; replaceable in tests.
var fileMtime = func(p string) (int64, bool) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, false
	}
	return fi.ModTime().UnixNano(), true
}

// lastWriter: the process that last finished writing p in this task, as far as the shim saw.
func lastWriter(t *Task, p string) int {
	pid := 0
	for _, o := range t.Ops {
		if o.Path == p && (o.Op == "c" || o.Op == "w") {
			pid = o.PID
		}
	}
	return pid
}
