package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The values of an R session (MH 21.11.): ktonrec writes, after each command, the hash of every
// variable it created or changed (or why it did not hash it: the person's filter by name, pattern,
// class or size). Hashes always; a value is saved, as an .rds file, only when the person marks it
// in the story. So the recorder can say what is new or changed and still unsaved, instead of
// saving everything again.

// parseVar reads one VARS entry: "desc|md5:<hex>", "desc|skip:<why>", or a bare desc (older ktonrec).
func parseVar(name, desc string) Var {
	v := Var{Name: name, Desc: desc}
	if d, h, ok := strings.Cut(desc, "|"); ok {
		v.Desc = d
		switch {
		case strings.HasPrefix(h, "md5:"):
			v.Hash = h
		case strings.HasPrefix(h, "skip:"):
			v.Skip = strings.TrimPrefix(h, "skip:")
		}
	}
	return v
}

// valueStates marks t's variables against the session's earlier tasks: new, changed or same.
func valueStates(earlier []*Task, t *Task) {
	last := map[string]string{} // name → its last hash
	seen := map[string]bool{}   // hashes the session had
	for _, x := range earlier {
		for _, v := range x.Vars {
			last[v.Name] = v.Hash
			if v.Hash != "" {
				seen[v.Hash] = true
			}
		}
	}
	for i := range t.Vars {
		v := &t.Vars[i]
		if v.Hash == "" {
			continue
		}
		prev, had := last[v.Name]
		switch {
		case !had:
			v.State = "new"
		case prev == v.Hash || seen[v.Hash]:
			v.State = "same"
		default:
			v.State = "changed"
		}
	}
}

// SavedValue: a value the person saved from the story, and where.
type SavedValue struct {
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Path    string `json:"path"`    // the .rds file, absolute
	Project string `json:"project"` // the recorded folder
	When    int64  `json:"when_ns"`
}

// Values is the store of saved values, by hash (values.json).
type Values struct {
	path string
	mu   sync.Mutex
	m    map[string]SavedValue
}

var valueHash = regexp.MustCompile(`^md5:[0-9a-f]{32}$`)

// OpenValues reads the store under dir.
func OpenValues(dir string) *Values {
	v := &Values{path: filepath.Join(dir, "values.json"), m: map[string]SavedValue{}}
	if b, err := os.ReadFile(v.path); err == nil {
		json.Unmarshal(b, &v.m)
	}
	return v
}

// Saved is where a value was saved, if it was.
func (v *Values) Saved(hash string) (SavedValue, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	x, ok := v.m[hash]
	return x, ok
}

// MarkSaved keeps that a value was saved to a file.
func (v *Values) MarkSaved(x SavedValue) error {
	if !valueHash.MatchString(x.Hash) {
		return errors.New("hash is md5:<32 hex>")
	}
	if x.Name == "" || !filepath.IsAbs(x.Path) || !filepath.IsAbs(x.Project) {
		return errors.New("want {hash, name, path (absolute), project (absolute)}")
	}
	if x.When == 0 {
		x.When = time.Now().UnixNano()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[x.Hash] = x
	if err := os.MkdirAll(filepath.Dir(v.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v.m, "", " ")
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

// StepValue is a variable a story step left, with what the person needs to decide to save it.
type StepValue struct {
	Var
	Saved string `json:"saved,omitempty"` // the file it was saved to, when it was
}
