package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
)

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
// command started. tasks must be copies (Checker.Annotate makes them).
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
		start := commandStart(t)
		for _, in := range t.Inputs {
			if len(in.From) > 0 || in.SHA256 == "" || in.SHA256 == EmptySHA256 {
				continue
			}
			var best *produced
			for i, p := range idx[in.Path+"\x00"+in.SHA256] {
				if p.end <= start {
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
			if last, ok := byPath[in.Path]; ok && last <= start && in.Status == "observed" {
				in.Status = "changed-outside"
			}
		}
	}
}

// commandStart: when the task's command began. A console task's Start is the previous
// command's end; when the extension says when this one was sent, that is later and right (a
// Python step in another session may have written its input in between).
func commandStart(t *Task) int64 {
	if len(t.Origins) > 0 && t.Origins[0].TS > t.Start {
		return t.Origins[0].TS
	}
	return t.Start
}

// Later is the next write of a file after a step used or wrote it: who wrote it again, and
// whether with the same bytes. The step's version is history then, not what the file holds.
// Unrecorded: no recorded step wrote it again, but the file has other bytes today (an editor,
// Excel, a copy): changed outside the recording.
type Later struct {
	Session    string `json:"session,omitempty"`
	Task       string `json:"task,omitempty"`
	Same       bool   `json:"same_bytes,omitempty"`
	Unrecorded bool   `json:"unrecorded,omitempty"`
}

// markLater sets Later on every input and output of a project's tasks (sorted by End) that a
// later step wrote again, across sessions, or that has other bytes today with no recorded
// write to explain it (Now from Checker.Annotate).
func markLater(tasks []*Task) {
	type write struct {
		t   *Task
		sum string
		at  int64 // when it was written: a long step may write early and end late
	}
	writes := map[string][]write{} // path → writes in order of time
	for _, t := range tasks {
		for _, o := range t.Outputs {
			at := o.WrittenAt
			if at == 0 {
				at = t.End
			}
			writes[o.Path] = append(writes[o.Path], write{t, o.SHA256, at})
		}
	}
	for _, ws := range writes {
		sort.SliceStable(ws, func(i, j int) bool { return ws[i].at < ws[j].at })
	}
	// next: the first later write, unless a later one changed the bytes: then that one, since a
	// recorded change explains today's file and a same-bytes rerun before it does not
	next := func(path, sum string, after int64, not *Task) *Later {
		var first *Later
		for _, w := range writes[path] {
			if w.t == not || w.at <= after {
				continue
			}
			if w.sum != sum {
				return &Later{Session: w.t.Session, Task: w.t.ID}
			}
			if first == nil {
				first = &Later{Session: w.t.Session, Task: w.t.ID, Same: true}
			}
		}
		return first
	}
	for _, t := range tasks {
		for _, in := range t.Inputs {
			read := in.LastRead
			if read == 0 {
				read = t.Start
			}
			in.Later = unrecorded(next(in.Path, in.SHA256, read, t), in.Now)
		}
		for _, o := range t.Outputs {
			at := o.WrittenAt
			if at == 0 {
				at = t.End
			}
			o.Later = unrecorded(next(o.Path, o.SHA256, at, t), o.Now)
		}
	}
}

// unrecorded: the file has other bytes today, and no recorded step explains it (none wrote it
// again with other bytes; next already prefers such a write): changed outside the recording.
func unrecorded(l *Later, now string) *Later {
	if now == NowChanged && (l == nil || l.Same) {
		return &Later{Unrecorded: true}
	}
	return l
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
	Registry string `json:"registry,omitempty"` // RegistryID of its cockpit repository, if it is in one
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
		if repo := RepoRoot(p.Path); isCockpitRepo(repo) {
			p.Registry = RegistryID(repo)
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

// RegistryID names a cockpit repository in URLs (/kton/reg/<id>/data/…) without its path.
func RegistryID(repo string) string {
	h := sha256.Sum256([]byte(repo))
	return hex.EncodeToString(h[:8])
}

func isCockpitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "cockpit.config.json"))
	return err == nil
}

// RegistryRepo is the cockpit repository with this RegistryID among the recorded projects'; ""
// when none. Only those are served, so the id cannot reach an arbitrary directory.
func (sv *Service) RegistryRepo(id string) string {
	for _, p := range sv.Projects() {
		if p.Registry == id {
			return RepoRoot(p.Path)
		}
	}
	return ""
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
