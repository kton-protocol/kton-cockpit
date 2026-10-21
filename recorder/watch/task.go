package watch

// Task is what happened between two markers of a session: one console command, one sourced
// file, one chunk. Everything here is an observation, not a declaration.
type Task struct {
	Script    string   `json:"script,omitempty"` // the session ran this file (R -f / Rscript)
	ScriptSHA string   `json:"script_sha256,omitempty"`
	Session   string   `json:"session,omitempty"` // set in views that span sessions (a whole project)
	ID        string   `json:"id"`
	Seq       int      `json:"seq"`
	Expr      string   `json:"expr"`
	Status    string   `json:"status"` // ok | err | - (tail after the last marker)
	Start     int64    `json:"start_ns"`
	End       int64    `json:"end_ns"`
	Origins   []Origin `json:"origins,omitempty"` // where the code came from (Positron, S6)
	Sources   []Source `json:"sources,omitempty"` // files run with source()/sys.source()
	Vars      []Var    `json:"vars,omitempty"`    // global variables the command created or changed (ktonrec)
	Uses      []VarUse `json:"uses,omitempty"`    // variables of earlier tasks this one used (from its code)

	Inputs     []*Input    `json:"inputs"`
	Outputs    []*Output   `json:"outputs"`
	Temporary  []string    `json:"temporary,omitempty"`
	Missing    []string    `json:"missing,omitempty"` // tried to read, did not exist
	Listings   []string    `json:"dir_listings,omitempty"`
	Commands   []Command   `json:"commands,omitempty"`
	Parents    map[int]int `json:"parents,omitempty"`    // pid → parent pid of every traced process, forks without exec too
	Ops        []FileOp    `json:"ops,omitempty"`        // reads and writes of project files by process, in order (for calls)
	Unrecorded []Command   `json:"unrecorded,omitempty"` // started but never entered (static binary)
	Network    []Net       `json:"network,omitempty"`
	EnvFiles   int         `json:"environment_files"`
	PIDs       []int       `json:"pids"`
}

// Input is one version of a file a task read.
type Input struct {
	Path      string `json:"path"`
	Class     Class  `json:"class"`
	SHA256    string `json:"sha256"`
	Status    string `json:"status"` // observed | hashed-by-watcher | unhashed-large | gone
	PID       int    `json:"pid"`
	From      []Ref  `json:"from,omitempty"`      // the earlier task that wrote this very file with these bytes
	SameAs    []Ref  `json:"same_as,omitempty"`   // other files earlier tasks wrote with the same bytes (not provenance)
	Rewritten bool   `json:"rewritten,omitempty"` // the same task wrote this file afterwards: a scratch file, not data
	Now       string `json:"now,omitempty"`       // same | changed | gone, compared with disk when shown
}

// Output is a file a task left behind, with its content at the task's end.
type Output struct {
	Path        string `json:"path"`
	Class       Class  `json:"class"`
	SHA256      string `json:"sha256"`
	Status      string `json:"status"` // observed | hashed-by-watcher | unhashed-large
	Overwritten bool   `json:"overwritten_later,omitempty"`
	Now         string `json:"now,omitempty"` // same | changed | gone, compared with disk when shown
}

// Ref points at an output of an earlier task.
type Ref struct {
	Session string `json:"session,omitempty"` // another session of the same project; empty: this one
	Task    string `json:"task"`
	Path    string `json:"path"`
}

type Command struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	Program string `json:"program"`
	Cmdline string `json:"cmdline"`
	Cwd     string `json:"cwd,omitempty"`
}

// FileOp is one read (r), opening for writing (w), finished write (c, with the hash it left) or
// removal (d: deleted, or renamed away)
// of a project file by one process. Unlike Inputs it keeps reads of files the task wrote itself:
// inside a task, a tool reads what another one wrote (PsN writes psn.mod, nmfe reads it).
type FileOp struct {
	PID     int    `json:"pid"`
	Op      string `json:"op"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Outside bool   `json:"outside,omitempty"` // not under the project
}

type Net struct {
	PID     int    `json:"pid"`
	Address string `json:"address"`
	Class   string `json:"class"` // loopback | unix | remote
	OK      bool   `json:"ok"`
}

// Var is a variable a command left in the global environment: name, class, shape.
type Var struct {
	Name string `json:"name"`
	Desc string `json:"desc"` // class:shape, e.g. data.frame:6x3
}

// Source is a file a task ran with source(); Class tells project files from R's own.
type Source struct {
	Path  string `json:"path"`
	Class Class  `json:"class"`
}

// Origin is where Positron says the executed code came from (onDidExecuteCode, S6).
type Origin struct {
	Kind      string `json:"kind"` // interactive | paste | script | notebook | …
	File      string `json:"file,omitempty"`
	StartLine int    `json:"start_line,omitempty"` // 1-based, 0 = unknown
	EndLine   int    `json:"end_line,omitempty"`
	Chunk     string `json:"chunk,omitempty"` // Quarto/Rmd chunk label or "#n"
	Code      string `json:"code,omitempty"`
	TS        int64  `json:"ts_ns"`
}
