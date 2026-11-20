// Package jobs is the checkout's jobs journal (ADR-016: .kton/local/jobs/<writer>.jsonl, never shared):
// one JSON line per state change of a run, folded per run id by its readers. The cockpit writes the
// workflow runs it starts; the machine's jobs runner (cockpit-runtime) writes its runs and ticks into
// the same place, one file per writer. The run record is the cockpit's account of a run, not a signed
// claim: the step records it names are.
package jobs

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/flock"
)

// States a run goes through.
const (
	Queued    = "queued"
	Running   = "running"
	Succeeded = "succeeded"
	Failed    = "failed"
	Cancelled = "cancelled"
)

// Kinds of lines: runs (workflow, poll, command) and the runner's ticks that start no run.
const (
	KindWorkflow = "workflow"
	KindPoll     = "poll"
	KindCommand  = "command"
	KindTick     = "tick"
)

// RotateAt is the size at which a writer's file is moved aside, as <writer>.<n>.jsonl.
const RotateAt = 16 << 20

// Ray names the package revision a workflow run runs.
type Ray struct {
	Package  string `json:"package"`
	Revision string `json:"revision"`
}

// PlanStep is one step's state within a run, what a runner resumes from after a crash.
type PlanStep struct {
	Step  string `json:"step"`
	State string `json:"state"`
	Foton string `json:"foton,omitempty"`
}

// Line is one state change of a run, or a tick.
type Line struct {
	ID     string          `json:"id,omitempty"`
	Kind   string          `json:"kind"`
	Ray    *Ray            `json:"ray,omitempty"`
	Origin json.RawMessage `json:"origin,omitempty"`
	State  string          `json:"state,omitempty"`
	At     string          `json:"at"`
	Steps  []string        `json:"steps,omitempty"`
	Plan   []PlanStep      `json:"plan,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	// Job and Outcome are a tick's: the scheduled job, and skipped|expired.
	Job     string `json:"job,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

// Run is a run as its lines fold: the last state, the first time it was seen and the time it ended.
type Run struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Ray    *Ray            `json:"ray,omitempty"`
	Origin json.RawMessage `json:"origin,omitempty"`
	State  string          `json:"state"`
	Start  string          `json:"start"`
	End    string          `json:"end,omitempty"`
	Steps  []string        `json:"steps,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Writer string          `json:"writer"`
}

// Terminal says a state is final.
func Terminal(state string) bool {
	return state == Succeeded || state == Failed || state == Cancelled
}

// NewID is a run id: the time it was made and eight random bytes, so ids sort by start.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "run-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

// Dir is the jobs journal of a checkout whose local directory is local.
func Dir(local string) string { return filepath.Join(local, "jobs") }

// Append writes one line to writer's file under dir, under the writer's lock, moving the file aside
// first when it has grown past RotateAt.
func Append(dir, writer string, l Line) error {
	if l.At == "" {
		l.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := flock.Lock(filepath.Join(dir, writer+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(dir, writer+".jsonl")
	if st, err := os.Stat(path); err == nil && st.Size() >= RotateAt {
		n := 1
		for {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s.%d.jsonl", writer, n))); os.IsNotExist(err) {
				break
			}
			n++
		}
		if err := os.Rename(path, filepath.Join(dir, fmt.Sprintf("%s.%d.jsonl", writer, n))); err != nil {
			return err
		}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Runs folds every writer's lines under dir into runs, the newest first. Ticks are left out, and so is
// a line that does not parse (a write cut short by a crash).
func Runs(dir string) ([]Run, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	// Rotated files first, oldest first, then the current one, so a run's lines are read in order.
	sort.Slice(files, func(i, j int) bool { return fileOrder(files[i]) < fileOrder(files[j]) })
	byID := map[string]*Run{}
	for _, path := range files {
		writer := writerOf(path)
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		rd := bufio.NewReader(f)
		for {
			b, rerr := rd.ReadBytes('\n')
			if len(b) == 0 && rerr != nil {
				if rerr != io.EOF {
					f.Close()
					return nil, rerr
				}
				break
			}
			var l Line
			if json.Unmarshal(b, &l) != nil || l.Kind == KindTick || l.ID == "" {
				continue // a tick, or a line that does not parse, whatever its length
			}
			r := byID[l.ID]
			if r == nil {
				r = &Run{ID: l.ID, Kind: l.Kind, Start: l.At, Writer: writer}
				byID[l.ID] = r
			}
			if l.Ray != nil {
				r.Ray = l.Ray
			}
			if len(l.Origin) > 0 {
				r.Origin = l.Origin
			}
			if l.State != "" {
				r.State = l.State
			}
			if l.Steps != nil {
				r.Steps = l.Steps
			}
			if len(l.Result) > 0 {
				r.Result = l.Result
			}
			if l.Error != "" {
				r.Error = l.Error
			}
			if Terminal(l.State) {
				r.End = l.At
			}
		}
		f.Close()
	}
	out := make([]Run, 0, len(byID))
	for _, r := range byID {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// fileOrder sorts <writer>.<n>.jsonl before <writer>.jsonl, and by n.
func fileOrder(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.LastIndex(base, "."); i > 0 {
		if n, err := strconv.Atoi(base[i+1:]); err == nil {
			return fmt.Sprintf("%s\x00%08d", base[:i], n)
		}
	}
	return base + "\x00~"
}

func writerOf(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.LastIndex(base, "."); i > 0 {
		if _, err := strconv.Atoi(base[i+1:]); err == nil {
			return base[:i]
		}
	}
	return base
}

// actKeys are the keys of an act origin, in the order a subtree is a prefix of.
var actKeys = []string{"package", "composition", "part", "act"}

// Matches says whether a run's origin falls under filter: a filter of kind act names a prefix of
// package, composition, part, act (a subtree of the composition tree); any other filter matches on
// every key it names. An empty filter matches every run.
func Matches(origin, filter json.RawMessage) bool {
	if len(filter) == 0 {
		return true
	}
	var f, o map[string]any
	if json.Unmarshal(filter, &f) != nil || len(f) == 0 {
		return true
	}
	if json.Unmarshal(origin, &o) != nil {
		return false
	}
	for k, v := range f {
		if fmt.Sprint(o[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// CheckActFilter refuses a filter of kind act that is not a prefix of package, composition, part, act:
// a part without its composition would match parts of that name anywhere.
func CheckActFilter(filter json.RawMessage) error {
	if len(filter) == 0 {
		return nil
	}
	var f map[string]any
	if err := json.Unmarshal(filter, &f); err != nil {
		return fmt.Errorf("origin filter is not a JSON object: %v", err)
	}
	if f["kind"] != "act" {
		return nil
	}
	gap := false
	for _, k := range actKeys {
		_, has := f[k]
		if has && gap {
			return fmt.Errorf("an act filter names a prefix of package, composition, part, act; %q without the keys before it", k)
		}
		if !has {
			gap = true
		}
	}
	return nil
}
