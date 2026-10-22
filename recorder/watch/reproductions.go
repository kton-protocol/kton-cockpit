package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Reproductions: the Positron extension runs a file's recipe in an empty directory and writes
// what came out, step by step, as kton-repro.json next to it (…/kton-cockpit/repro/<run>/). The
// story shows them: when the file was reproduced, and which step gave the same bytes.

// Reproduction is one run of a recipe.
type Reproduction struct {
	Project    string      `json:"project"`
	Target     string      `json:"target"` // absolute
	Recorded   string      `json:"recorded"`
	Reproduced string      `json:"reproduced"` // "" when the recipe did not produce it
	Same       bool        `json:"same"`
	When       time.Time   `json:"when"`
	Dir        string      `json:"dir"`
	Seconds    int         `json:"seconds"`
	Notes      []string    `json:"notes,omitempty"`
	Steps      []ReproStep `json:"steps,omitempty"`
}

// ReproStep is what one recorded step of the recipe produced in the reproduction.
type ReproStep struct {
	Session string        `json:"session"`
	Task    string        `json:"task"`
	Outputs []ReproOutput `json:"outputs"`
}

type ReproOutput struct {
	Path       string `json:"path"` // absolute, in the project
	Recorded   string `json:"recorded"`
	Reproduced string `json:"reproduced"` // "" when not produced
	Same       bool   `json:"same"`
}

// StepRepro is what the latest reproduction says about one story step.
type StepRepro struct {
	When        time.Time `json:"when"`
	Same        bool      `json:"same"`                   // every output it had came out with the same bytes
	Different   []string  `json:"different,omitempty"`    // project-relative
	NotProduced []string  `json:"not_produced,omitempty"` // project-relative
}

// reproDir: where the extension keeps reproductions, beside the logs (…/kton-cockpit/repro).
func (sv *Service) reproDir() string { return filepath.Join(filepath.Dir(sv.Dir), "repro") }

// Reproductions of one file of a project, newest first.
func (sv *Service) Reproductions(project, target string) []Reproduction {
	files, _ := filepath.Glob(filepath.Join(sv.reproDir(), "*", "kton-repro.json"))
	var out []Reproduction
	for _, f := range files {
		b, err := os.ReadFile(f)
		var r Reproduction
		if err != nil || json.Unmarshal(b, &r) != nil || r.Project != project || r.Target != target {
			continue // also older runs, which named neither
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

// attachRepro gives the story's steps what the latest reproduction says about them.
func attachRepro(st *Story, rs []Reproduction, rel func(string) string) {
	st.Reproductions = rs
	if len(rs) == 0 {
		return
	}
	last := rs[0]
	by := map[string]ReproStep{}
	for _, s := range last.Steps {
		by[s.Session+"\x00"+s.Task] = s
	}
	for i := range st.Steps {
		s, ok := by[st.Steps[i].Session+"\x00"+st.Steps[i].Task]
		if !ok {
			continue
		}
		r := &StepRepro{When: last.When, Same: true}
		for _, o := range s.Outputs {
			switch {
			case o.Reproduced == "":
				r.Same = false
				r.NotProduced = append(r.NotProduced, rel(o.Path))
			case !o.Same:
				r.Same = false
				r.Different = append(r.Different, rel(o.Path))
			}
		}
		st.Steps[i].Repro = r
	}
}
