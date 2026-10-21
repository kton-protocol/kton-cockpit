package watch

import (
	"path/filepath"
	"sort"
	"strings"
)

// A reproduction plan for one file version, built from the calls: the call that made the file
// runs again (that is the point of reproducing it); each of its inputs is either taken from the
// CAS, or made again by the call that made it, recursively; what is neither is lost, and the
// plan says which file. A call inside another planned call runs as part of that one.

// ReproFile is a file put into the working directory before anything runs.
type ReproFile struct {
	Rel    string `json:"rel"` // relative to the project
	SHA256 string `json:"sha256"`
}

// ReproRun is a call to run, in the order given.
type ReproRun struct {
	Tool   string   `json:"tool"`
	Argv   []string `json:"argv"`
	Cwd    string   `json:"cwd"` // relative to the project
	Task   string   `json:"task"`
	Key    string   `json:"key"`
	Target bool     `json:"target,omitempty"` // the call that made the file: never reused
	Dirs   []string `json:"dirs,omitempty"`   // directories it writes into, relative to the project: they existed then
}

// ReproPlan is what kton-repro carries out.
type ReproPlan struct {
	Project string      `json:"project"`
	Target  ReproFile   `json:"target"`
	Put     []ReproFile `json:"put"`
	Run     []ReproRun  `json:"run"`
	Lost    []string    `json:"lost,omitempty"` // inputs neither kept nor producible: the plan cannot run
}

func reproPlanOf(calls []Call, has func(sum string) bool, project, path, sum string) *ReproPlan {
	rel := func(p string) string {
		if r, err := filepath.Rel(project, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}
	// the first call that wrote each version at its path; by content alone only as a fallback
	// (PsN copies the data into its own directory: same bytes, not the data's maker)
	atPath, bySum := map[string]int{}, map[string]int{}
	latest := ""
	for _, c := range calls {
		for _, w := range c.Writes {
			if w.Path == path {
				latest = w.SHA256 // calls are in time order: the last one seen is the latest
			}
		}
	}
	for i := len(calls) - 1; i >= 0; i-- { // backwards, so the first writer wins
		for _, w := range calls[i].Writes {
			atPath[w.Path+"\x00"+w.SHA256] = i
			bySum[w.SHA256] = i
		}
	}
	maker := func(f CallFile) (int, bool) {
		if i, ok := atPath[f.Path+"\x00"+f.SHA256]; ok {
			return i, true
		}
		i, ok := bySum[f.SHA256]
		return i, ok
	}
	if sum == "" {
		sum = latest
	}
	ti, ok := atPath[path+"\x00"+sum]
	if !ok {
		return nil
	}
	plan := &ReproPlan{Project: project, Target: ReproFile{Rel: rel(path), SHA256: sum}}
	run := map[int]bool{}
	put := map[string]bool{}
	lost := map[string]bool{}
	var need func(i int, depth int)
	need = func(i int, depth int) {
		if run[i] || depth > 64 {
			return
		}
		run[i] = true
		for _, in := range calls[i].Reads {
			switch p, made := maker(in); {
			case has(in.SHA256):
				if !put[in.Path+"\x00"+in.SHA256] {
					put[in.Path+"\x00"+in.SHA256] = true
					plan.Put = append(plan.Put, ReproFile{Rel: rel(in.Path), SHA256: in.SHA256})
				}
			case made && p != i:
				need(p, depth+1)
			default:
				lost[rel(in.Path)] = true
			}
		}
	}
	need(ti, 0)
	inside := func(i int) bool { // a call inside another planned call runs as part of it
		c := calls[i]
		for j := range run {
			if j != i && calls[j].Session == c.Session && calls[j].Task == c.Task && c.Parent == calls[j].PID {
				return true
			}
		}
		return false
	}
	for i, c := range calls {
		if !run[i] || inside(i) {
			continue
		}
		dirs := map[string]bool{}
		var ds []string
		for _, w := range c.Writes {
			if d := filepath.Dir(rel(w.Path)); !dirs[d] {
				dirs[d] = true
				ds = append(ds, d)
			}
		}
		plan.Run = append(plan.Run, ReproRun{Tool: c.Tool, Argv: c.Argv, Cwd: rel(c.Cwd), Task: c.Task, Key: c.Key, Target: i == ti, Dirs: ds})
	}
	for f := range lost {
		plan.Lost = append(plan.Lost, f)
	}
	sort.Strings(plan.Lost)
	return plan
}

// ReproPlan for a file version of a project (sum may be empty: the latest version a call wrote).
func (sv *Service) ReproPlan(project, path, sum string) *ReproPlan {
	has := func(string) bool { return false }
	if sv.CAS != nil {
		has = sv.CAS.Has
	}
	return reproPlanOf(sv.ProjectCalls(project), has, project, path, sum)
}
