package watch

import "sort"

// A working day is several sessions: the morning's R session writes a dataset, the
// afternoon's reads it. Within a session the watcher links that itself; across sessions the
// service does it when showing, from all sessions of the same project.

type produced struct {
	ref Ref
	end int64
}

// crossIndex: path+hash → outputs of every session of the project except one. Caller holds
// the read lock.
func (sv *Service) crossIndex(project, except string) map[string][]produced {
	idx := map[string][]produced{}
	for id, tr := range sv.sessions {
		s := tr.s
		if id == except || s.Project != project || project == "" {
			continue
		}
		for _, t := range s.Tasks {
			for _, o := range t.Outputs {
				if o.SHA256 == "" || o.SHA256 == EmptySHA256 {
					continue
				}
				k := o.Path + "\x00" + o.SHA256
				idx[k] = append(idx[k], produced{Ref{Session: id, Task: t.ID, Path: o.Path}, t.End})
			}
		}
	}
	for _, v := range idx {
		sort.Slice(v, func(i, j int) bool { return v[i].end < v[j].end })
	}
	return idx
}

// linkAcross fills From for inputs that the session itself cannot explain, with the latest
// output of the same file and bytes that another session of the project wrote before the
// task started. tasks must be copies (Checker.Annotate makes them).
func (sv *Service) linkAcross(s *Session, tasks []*Task) {
	idx := sv.crossIndex(s.Project, s.ID)
	if len(idx) == 0 {
		return
	}
	byPath := map[string]int64{} // path → when another session last wrote it
	for _, ps := range idx {
		for _, p := range ps {
			if p.end > byPath[p.ref.Path] {
				byPath[p.ref.Path] = p.end
			}
		}
	}
	for _, t := range tasks {
		for _, in := range t.Inputs {
			if len(in.From) > 0 || in.SHA256 == "" || in.SHA256 == EmptySHA256 {
				continue
			}
			var best *produced
			for i, p := range idx[in.Path+"\x00"+in.SHA256] {
				if p.end <= t.Start {
					best = &idx[in.Path+"\x00"+in.SHA256][i]
				}
			}
			if best != nil {
				in.From = []Ref{best.ref}
				if in.Status == "changed-outside" {
					in.Status = "observed"
				}
				continue
			}
			// another session wrote this file earlier, with other bytes: changed by hand since
			if last, ok := byPath[in.Path]; ok && last <= t.Start && in.Status == "observed" {
				in.Status = "changed-outside"
			}
		}
	}
}

// lineageAcross walks back like Lineage, following references into other sessions.
func (sv *Service) lineageAcross(sessionID, file string) *Step {
	tr, ok := sv.sessions[sessionID]
	if !ok {
		return nil
	}
	check := NewChecker()
	views := map[string][]*Task{}
	view := func(id string) []*Task {
		if v, ok := views[id]; ok {
			return v
		}
		t, ok := sv.sessions[id]
		if !ok {
			return nil
		}
		v := check.Annotate(t.s.Tasks)
		sv.linkAcross(t.s, v)
		views[id] = v
		return v
	}
	find := func(id, task string) *Task {
		for _, t := range view(id) {
			if t.ID == task {
				return t
			}
		}
		return nil
	}
	var producer *Task
	for _, t := range view(tr.s.ID) {
		for _, o := range t.Outputs {
			if o.Path == file {
				producer = t
			}
		}
	}
	if producer == nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(sid string, t *Task, file string) *Step
	walk = func(sid string, t *Task, file string) *Step {
		st := &Step{Session: sid, Task: t, File: file}
		key := sid + "\x00" + t.ID + "\x00" + file
		if seen[key] {
			return st
		}
		seen[key] = true
		for _, in := range t.Inputs {
			for _, r := range in.From {
				rs := r.Session
				if rs == "" {
					rs = sid
				}
				if up := find(rs, r.Task); up != nil {
					st.From = append(st.From, walk(rs, up, r.Path))
				}
			}
		}
		return st
	}
	return walk(tr.s.ID, producer, file)
}

// ProjectSummary is one project with the sessions recorded for it.
type ProjectSummary struct {
	Path     string `json:"path"`
	Sessions int    `json:"sessions"`
	Tasks    int    `json:"tasks"`
	Last     int64  `json:"last_ns"`
}

// Projects lists the projects that have sessions, most recent first.
func (sv *Service) Projects() []ProjectSummary {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	by := map[string]*ProjectSummary{}
	for _, tr := range sv.sessions {
		s := tr.s
		if s.Project == "" {
			continue
		}
		p := by[s.Project]
		if p == nil {
			p = &ProjectSummary{Path: s.Project}
			by[s.Project] = p
		}
		p.Sessions++
		p.Tasks += len(s.Tasks)
		if s.Last > p.Last {
			p.Last = s.Last
		}
	}
	var out []ProjectSummary
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

func (sv *Service) projectView(check *Checker, project string, f Filter) map[string]any {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	var all []*Task
	total := 0
	for id, tr := range sv.sessions {
		s := tr.s
		if s.Project != project {
			continue
		}
		total += len(s.Tasks)
		for _, t := range sv.annotated(check, s, f.View(s)) {
			t.Session = id
			all = append(all, t)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].End < all[j].End })
	return map[string]any{"id": "project:" + project, "project": project, "total": total, "tasks": all}
}
