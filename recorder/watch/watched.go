package watch

// Watched (MH 18.11.: "we need more control over what is watched and which sessions to keep"): the
// folders the recorder records, and the sessions kept whatever their age.
//
//   - Folders: a session is stored only when the directory its first command ran in (or, for an R
//     console, its project) lies in one of them. Without the file, everything is recorded, as before.
//   - The same folders go to HashFile, colon-separated: the shim reads them from there when
//     KTON_TRACE_HASH is "@<HashFile>", so a folder added or removed counts for the next process,
//     without changing any editor setting.
//   - Keep: session ids `ktonwatch prune` and -max-age leave alone (the person pinned them, or
//     published from them).
//
// It lives in <store>/watched/watched.json: not *.json beside the sessions, those are read as sessions.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Watched struct {
	File     string `json:"-"` // <store>/watched/watched.json
	HashFile string `json:"-"` // the shim's prefix file; "" writes none

	mu      sync.Mutex
	set     bool     // the file exists: Folders rule; without it everything is recorded
	Folders []string `json:"folders"`
	Keep    []string `json:"keep"`
}

// OpenWatched reads the list; a missing or unreadable file means "everything", as before.
func OpenWatched(store, hashFile string) *Watched {
	w := &Watched{File: filepath.Join(store, "watched", "watched.json"), HashFile: hashFile}
	if b, err := os.ReadFile(w.File); err == nil && json.Unmarshal(b, w) == nil {
		w.set = true
		for i, f := range w.Folders {
			w.Folders[i] = filepath.Clean(f)
		}
	}
	return w
}

// WatchedState is what the API reports.
type WatchedState struct {
	Set     bool     `json:"set"` // false: no list yet, everything is recorded
	Folders []string `json:"folders"`
	Keep    []string `json:"keep"`
}

func (w *Watched) State() WatchedState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WatchedState{Set: w.set, Folders: append([]string{}, w.Folders...), Keep: append([]string{}, w.Keep...)}
}

func inFolder(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// Covers: is a path in a watched folder (always, without a list)
func (w *Watched) Covers(p string) bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.set {
		return true
	}
	p = filepath.Clean(p)
	for _, f := range w.Folders {
		if inFolder(p, f) {
			return true
		}
	}
	return false
}

// Kept: did the person keep this session
func (w *Watched) Kept(id string) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, k := range w.Keep {
		if k == id {
			return true
		}
	}
	return false
}

// Wanted: should a session be stored, by where it ran (s.Where, else its project) or because it is kept.
// A session not yet placed is wanted until it is.
func (w *Watched) Wanted(s *Session) bool {
	if w == nil || !w.State().Set || w.Kept(s.ID) {
		return true
	}
	where := s.Where
	if where == "" {
		where = s.Project
	}
	if where == "" {
		return !s.Closed
	}
	return w.Covers(where)
}

// SetFolder adds (on) or removes a folder, makes the list rule from now on, and writes the files.
func (w *Watched) SetFolder(folder string, on bool) error {
	folder = filepath.Clean(folder)
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []string{}
	for _, f := range w.Folders {
		if f != folder {
			out = append(out, f)
		}
	}
	if on {
		out = append(out, folder)
	}
	sort.Strings(out)
	w.Folders, w.set = out, true
	return w.save()
}

// SetKeep keeps a session (on) or lets it go.
func (w *Watched) SetKeep(id string, on bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []string{}
	for _, k := range w.Keep {
		if k != id {
			out = append(out, k)
		}
	}
	if on {
		out = append(out, id)
	}
	sort.Strings(out)
	w.Keep = out
	return w.save()
}

func (w *Watched) save() error {
	if err := os.MkdirAll(filepath.Dir(w.File), 0o755); err != nil {
		return err
	}
	if w.Folders == nil {
		w.Folders = []string{}
	}
	if w.Keep == nil {
		w.Keep = []string{}
	}
	b, _ := json.MarshalIndent(w, "", " ")
	if err := writeAtomic(w.File, append(b, '\n')); err != nil {
		return err
	}
	if w.set && w.HashFile != "" {
		return writeAtomic(w.HashFile, []byte(strings.Join(w.Folders, ":")+"\n"))
	}
	return nil
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
