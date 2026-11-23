package watch

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Model derivations from an interactive pharmpy session (python/ktonpharmpy.py, MH 21.11.): one
// DERIVE line per modeling call that took or gave a model. The shim sees files only; these say
// that run7.ctl, written in the console, was developed from run1.ctl by add_lag_time. A step that
// wrote a model file this way is a child step (pmx/parent-step) of the step that made the file it
// was read from.

// Derive is one pharmpy call as the recorder wrote it.
type Derive struct {
	Fn   string   `json:"fn"`
	In   []string `json:"in,omitempty"`   // hashes of the models it took
	Out  string   `json:"out,omitempty"`  // hash of the model it gave
	Name string   `json:"name,omitempty"` // the model's name
	Path string   `json:"path,omitempty"` // read_model: the file read; write_model: the file written
}

// ModelOrigin: a model file a step wrote, and the file its model was developed from.
type ModelOrigin struct {
	Output  string   `json:"output"`                // the file written (repo-relative)
	From    string   `json:"from"`                  // the file the model was read from (repo-relative when in the repo)
	FromSHA string   `json:"from_sha256,omitempty"` // the bytes it had when read (the shim's hash of that read)
	Via     []string `json:"via"`                   // the calls in between, in order (add_lag_time, set_name, …)
}

func parseDerive(e Event) (Derive, bool) {
	var d Derive
	if err := json.Unmarshal([]byte(e.Extra), &d); err != nil || d.Fn == "" {
		return Derive{}, false
	}
	if e.Path != "" && e.Path != "-" {
		d.Path = e.Path
	}
	return d, true
}

// ModelOrigins: for each model file the task wrote through pharmpy, the file it was developed
// from, followed back through the session's calls up to this task. A model never read from a
// file (built in the session) has no origin.
func ModelOrigins(s *Session, t *Task, repo string) []ModelOrigin {
	return modelOriginsIn(s.Tasks, t, repo)
}

// modelOriginsIn: the same over a session's tasks in their order (other sessions' tasks are skipped).
func modelOriginsIn(tasks []*Task, t *Task, repo string) []ModelOrigin {
	made := map[string]Derive{} // model hash → the call that gave it (the latest one)
	read := map[string]string{} // file path → the shim's sha256 of its read, in the tasks so far
	var writes []Derive
	for _, x := range tasks {
		if x.Session != t.Session {
			continue
		}
		if x.ID == t.ID { // its own calls with what it read (the task as annotated)
			x = t
		}
		for _, in := range x.Inputs {
			read[in.Path] = in.SHA256
		}
		for _, d := range x.Derives {
			if d.Out != "" {
				if _, seen := made[d.Out]; !seen || d.Fn != "read_model" {
					made[d.Out] = d
				}
			}
			if x.ID == t.ID && d.Fn == "write_model" && d.Path != "" && len(d.In) > 0 {
				writes = append(writes, d)
			}
		}
		if x.ID == t.ID { // the task as annotated may be a copy: by its id
			break
		}
	}
	rel := func(p string) string {
		if r, err := filepath.Rel(repo, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return p
	}
	var out []ModelOrigin
	for _, w := range writes {
		var via []string
		h, guard := w.In[0], 0
		for guard < 1000 {
			guard++
			d, ok := made[h]
			if !ok {
				break
			}
			if d.Fn == "read_model" {
				if d.Path != "" && d.Path != w.Path {
					for i, j := 0, len(via)-1; i < j; i, j = i+1, j-1 {
						via[i], via[j] = via[j], via[i]
					}
					out = append(out, ModelOrigin{Output: rel(w.Path), From: rel(d.Path), FromSHA: read[d.Path], Via: via})
				}
				break
			}
			via = append(via, d.Fn)
			if len(d.In) == 0 {
				break // built in the session, from no model
			}
			h = d.In[0]
		}
	}
	return out
}
