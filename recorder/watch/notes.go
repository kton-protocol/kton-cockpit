package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// What a person (or Claude, drafting for them) says about a step of a story: a description and a
// rationale, unsigned. They become claims only when the person publishes the step and signs them
// (`cockpit say describes|rationale`). Kept here rather than in one editor window, so the story tab
// and the MCP tools see the same drafts.
//
// Keys are the story's: "<session>/<task>" for a recorded step, "hand/<file>@<sha256>" for a
// change made by hand between two steps.

// Note is the draft for one step.
type Note struct {
	Description string `json:"description,omitempty"`
	Rationale   string `json:"rationale,omitempty"`
	By          string `json:"by,omitempty"`      // who wrote it last: "you", "claude", …
	When        int64  `json:"when_ns,omitempty"` // when it was written last
}

// Notes is the store: one JSON file, written whole on every change.
type Notes struct {
	path string
	mu   sync.Mutex
	m    map[string]Note
}

// OpenNotes reads the notes under dir (created when the first note is written).
func OpenNotes(dir string) *Notes {
	n := &Notes{path: filepath.Join(dir, "notes.json"), m: map[string]Note{}}
	if b, err := os.ReadFile(n.path); err == nil {
		json.Unmarshal(b, &n.m)
	}
	return n
}

// All is every note whose key starts with one of the prefixes (all of them without a prefix).
func (n *Notes) All(prefixes ...string) map[string]Note {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := map[string]Note{}
	for k, v := range n.m {
		if len(prefixes) == 0 {
			out[k] = v
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				out[k] = v
				break
			}
		}
	}
	return out
}

// Get is the note of one step.
func (n *Notes) Get(key string) (Note, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	v, ok := n.m[key]
	return v, ok
}

// Put sets a step's note; an empty description and rationale remove it.
func (n *Notes) Put(key string, note Note) error {
	if note.When == 0 {
		note.When = time.Now().UnixNano()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if strings.TrimSpace(note.Description) == "" && strings.TrimSpace(note.Rationale) == "" {
		delete(n.m, key)
	} else {
		n.m[key] = note
	}
	return n.save()
}

func (n *Notes) save() error {
	if err := os.MkdirAll(filepath.Dir(n.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(n.m, "", " ")
	if err != nil {
		return err
	}
	tmp := n.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, n.path)
}
