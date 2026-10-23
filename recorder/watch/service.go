package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Service follows every log in a trace directory, one session per *.tsv, and keeps the
// resulting observations in a store directory (one JSON file per session). It is the
// recorder's only state, and it is a cache: delete the store and the logs rebuild it.
type Service struct {
	Dir           string // where shims write logs (…/kton-cockpit/trace)
	Store         string // where observations are kept (…/kton-cockpit/observations)
	Idle          time.Duration
	Web           string // a kton-web checkout: its graph viewer draws the observations
	NewClassifier func() *Classifier
	Logf          func(format string, args ...any)

	// CAS, when set, keeps the inputs and outputs of every tool call by their hash.
	CAS *CAS

	// Placer, when set, finds where a story's sources reside (asks the cockpit).
	Placer *Placer

	// Notes are the drafts of descriptions and rationales per story step (Load opens them).
	Notes *Notes

	// UI passes what the MCP tools ask of the editor to the listening windows (Load makes it).
	UI *UIHub

	allowedMu sync.Mutex
	allowed   map[string]string // registry id → a repository the MCP tools admitted (AllowRegistry)

	mu       sync.RWMutex
	archived map[string]bool // session\x00task whose call files were handed to the CAS
	callMemo sync.Map        // the calls of a task, until it grows (callsOf)
	sessions map[string]*tracked
	origins  []Origin
	oSize    int64
}

type tracked struct {
	s        *Session
	f        *os.File
	r        *bufio.Reader
	partial  string
	lastData time.Time
	dirty    bool
}

// OriginsFile is where the Positron extension writes executions, next to the logs.
const OriginsFile = "positron-exec.jsonl"

func (sv *Service) logf(format string, args ...any) {
	if sv.Logf != nil {
		sv.Logf(format, args...)
	}
}

// Load reads sessions a previous run stored whose log is gone. A session whose log still
// exists is read again from the log, so a newer watcher's rules apply to it: the store is a
// cache, the logs are the record.
func (sv *Service) Load() error {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.sessions == nil {
		sv.sessions = map[string]*tracked{}
	}
	if sv.UI == nil {
		sv.UI = NewUIHub()
	}
	if sv.Notes == nil && sv.Store != "" {
		sv.Notes = OpenNotes(filepath.Join(sv.Store, "notes")) // not *.json beside the sessions: those are read as sessions
	}
	files, _ := filepath.Glob(filepath.Join(sv.Store, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := &Session{}
		if json.Unmarshal(b, s) != nil || !s.Closed {
			continue
		}
		if _, err := os.Stat(s.Log); err == nil {
			continue
		}
		s.cls = &Classifier{Project: s.Project}
		sv.sessions[s.ID] = &tracked{s: s}
	}
	return nil
}

// Run polls until ctx ends.
func (sv *Service) Run(ctx context.Context, every time.Duration) error {
	if err := os.MkdirAll(sv.Dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(sv.Store, 0o755); err != nil {
		return err
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		sv.Poll(time.Now())
		sv.mu.Lock()
		pending := sv.collectPending()
		sv.mu.Unlock()
		sv.keep(pending) // without the lock: it reads files
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Poll does one round: new logs, new lines, timeouts, origins, store.
func (sv *Service) Poll(now time.Time) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.sessions == nil {
		sv.sessions = map[string]*tracked{}
	}
	logs, _ := filepath.Glob(filepath.Join(sv.Dir, "*.tsv"))
	for _, l := range logs {
		id := strings.TrimSuffix(filepath.Base(l), ".tsv")
		if _, ok := sv.sessions[id]; ok {
			continue
		}
		f, err := os.Open(l)
		if err != nil {
			continue
		}
		cls := sv.NewClassifier()
		cls.Log = l
		sv.sessions[id] = &tracked{s: NewSession(id, l, cls), f: f, r: bufio.NewReader(f), lastData: now}
		sv.logf("session %s: %s", id, l)
	}
	if fi, err := os.Stat(filepath.Join(sv.Dir, OriginsFile)); err == nil && fi.Size() != sv.oSize {
		if o, err := ParseOrigins(filepath.Join(sv.Dir, OriginsFile)); err == nil {
			sv.origins, sv.oSize = o, fi.Size()
			for _, tr := range sv.sessions {
				tr.dirty = true
			}
		}
	}
	for id, tr := range sv.sessions {
		if tr.f == nil {
			continue // loaded from the store, closed
		}
		before := len(tr.s.Tasks)
		got := sv.read(tr)
		if got {
			tr.lastData = now
		}
		if !tr.s.Closed && sv.Idle > 0 && now.Sub(tr.lastData) > sv.Idle && !anyAlive(tr.s.PIDs()) {
			tr.s.Close("timeout", tr.s.Last)
			sv.logf("session %s: closed after %s without data, no process left", id, sv.Idle)
		}
		if len(tr.s.Tasks) != before || tr.s.Closed {
			tr.dirty = true
		}
		if tr.s.Closed {
			tr.f.Close()
			tr.f = nil
		}
	}
	for _, tr := range sv.sessions {
		if tr.dirty {
			AttachOrigins(tr.s.Tasks, OriginsIn(sv.origins, tr.s.Language))
			LinkVars(tr.s.Tasks)
			if err := sv.save(tr.s); err != nil {
				sv.logf("store %s: %v", tr.s.ID, err)
			}
			tr.dirty = false
		}
	}
}

func (sv *Service) read(tr *tracked) bool {
	got := false
	for {
		line, err := tr.r.ReadString('\n')
		if line != "" {
			got = true
		}
		if err != nil {
			tr.partial += line // incomplete last line: wait for the rest
			if !errors.Is(err, io.EOF) {
				sv.logf("read %s: %v", tr.s.Log, err)
			}
			return got
		}
		tr.s.Line(tr.partial + line)
		tr.partial = ""
		if tr.s.Closed {
			return got
		}
	}
}

func anyAlive(pids []int) bool {
	for _, p := range pids {
		if p > 0 && alive(p) {
			return true
		}
	}
	return false
}

func (sv *Service) save(s *Session) error {
	if sv.Store == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(sv.Store, "."+s.ID+".json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(sv.Store, s.ID+".json"))
}

// Summary is one line of the session list.
type Summary struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Start   int64  `json:"start_ns"`
	Last    int64  `json:"last_ns"`
	Closed  bool   `json:"closed"`
	Tasks   int    `json:"tasks"`
}

// Sessions lists all sessions, newest first.
func (sv *Service) Sessions() []Summary {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	var out []Summary
	for _, tr := range sv.sessions {
		s := tr.s
		out = append(out, Summary{s.ID, s.Project, s.Start, s.Last, s.Closed, len(s.Tasks)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

// With runs fn with a session under the read lock.
func (sv *Service) With(id string, fn func(*Session)) bool {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	tr, ok := sv.sessions[id]
	if !ok {
		return false
	}
	fn(tr.s)
	return true
}
