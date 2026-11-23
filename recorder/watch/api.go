package watch

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxBlob bounds what /kton/blob reads into memory to serve a file preview.
const maxBlob = 256 << 20

var errTooLarge = errors.New("too large")

func readAtMost(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, n+1))
	if err == nil && int64(len(b)) > n {
		return nil, errTooLarge
	}
	return b, err
}

//go:embed ui/index.html
var indexHTML []byte

// Handler serves the observations read-only. Writing (documenting, persisting) goes through
// the cockpit's verbs, never through here.
//
//	GET /                          the viewer
//	GET /api/sessions              all sessions, newest first
//	GET /api/sessions/{id}?noise=1&effect=1&tool=nmfe&file=step3
//	GET /api/lineage?session=ID&file=/abs/path
//	GET /api/draft?session=ID&task=N   proposal for `cockpit publish` (repo-relative paths, cmd)
//	GET /api/files?project=P                    file versions with their role (source, intermediate, result)
//	GET /api/recipe?project=P&path=F[&sha=H]    how F came about, with a verdict and a script
//	GET /api/descendants?project=P&path=F[&sha=H] what became of F
//	GET /api/calls?project=P                    the tool calls of a project, with their keys
//	GET /api/calls?key=K                        every recorded call with this key, in any project
//	GET /api/calls?session=ID&task=N           the tool calls of one task
//	GET /api/calldraft?session=ID&task=N&pid=P  proposal for `cockpit publish` of one tool call
//	GET /api/calls?argv=["nmfe75","run1.mod"]   recorded calls with this command line, newest first
//	    …&wrote=F                                only calls that wrote F (sha256, absolute or relative path)
//	    …&brief=1                                without reads, outside and missing
//	    …&offset=N&limit=M                       a page; X-Total-Count says how many there are
//	GET /api/producible?project=P               per file version of the calls: kept, producible again, or lost
//	GET /api/reproplan?project=P&path=F[&sha=H] what to put in place and which calls to run to make F again
func (sv *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	check := NewChecker()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sv.Sessions())
	})
	mux.HandleFunc("GET /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := Filter{
			HideNoise:  q.Get("noise") == "1",
			OnlyEffect: q.Get("effect") == "1",
			Tool:       strings.TrimSpace(q.Get("tool")),
			File:       strings.TrimSpace(q.Get("file")),
		}
		var out any
		ok := sv.With(r.PathValue("id"), func(s *Session) {
			out = map[string]any{
				"id": s.ID, "project": s.Project, "closed": s.Closed, "close_reason": s.CloseReason,
				"start_ns": s.Start, "last_ns": s.Last, "total": len(s.Tasks), "tasks": sv.annotated(check, s, f.View(s)),
			}
		})
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sv.Projects())
	})
	// all sessions of one project in time order: a working day in one view
	mux.HandleFunc("GET /api/project", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := Filter{HideNoise: q.Get("noise") == "1", OnlyEffect: q.Get("effect") == "1",
			Tool: strings.TrimSpace(q.Get("tool")), File: strings.TrimSpace(q.Get("file"))}
		writeJSON(w, sv.projectView(check, q.Get("path"), f))
	})
	// file-first: the files of a project, how one came about, what became of one
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sv.Files(r.URL.Query().Get("project")))
	})
	mux.HandleFunc("GET /api/recipe", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if rc := sv.Recipe(q.Get("project"), q.Get("path"), q.Get("sha")); rc != nil {
			writeJSON(w, rc)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /api/calls", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var out []Call
		switch {
		case q.Get("key") != "":
			out = sv.CallsByKey(q.Get("key"))
		case q.Get("argv") != "":
			var argv []string
			if json.Unmarshal([]byte(q.Get("argv")), &argv) != nil {
				http.Error(w, "argv: a JSON array of strings", http.StatusBadRequest)
				return
			}
			out = sv.CallsByArgv(argv)
		case q.Get("project") != "":
			out = sv.ProjectCalls(q.Get("project"))
		case q.Get("session") != "" && q.Get("task") != "":
			sv.With(q.Get("session"), func(s *Session) {
				for _, t := range s.Tasks {
					if t.ID == q.Get("task") {
						out = sv.callsOf(s.ID, t)
					}
				}
			})
		default:
			http.Error(w, "project, key, argv, or session and task", http.StatusBadRequest)
			return
		}
		if f := q.Get("wrote"); f != "" {
			kept := out[:0:0]
			for _, c := range out {
				if c.Wrote(f) {
					kept = append(kept, c)
				}
			}
			out = kept
		}
		w.Header().Set("X-Total-Count", strconv.Itoa(len(out)))
		if n, err := strconv.Atoi(q.Get("offset")); err == nil && n > 0 {
			out = out[min(n, len(out)):]
		}
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n >= 0 && n < len(out) {
			out = out[:n]
		}
		if q.Get("brief") != "" {
			b := make([]Call, len(out))
			for i, c := range out {
				b[i] = c.Brief()
			}
			out = b
		}
		if out == nil {
			out = []Call{}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/reproplan", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if p := sv.ReproPlan(q.Get("project"), q.Get("path"), q.Get("sha")); p != nil {
			writeJSON(w, p)
			return
		}
		http.Error(w, "no recorded call wrote this file", http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/producible", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sv.ProjectProducible(r.URL.Query().Get("project")))
	})
	mux.HandleFunc("GET /api/story", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if st := sv.Story(q.Get("project"), q.Get("path"), q.Get("sha")); st != nil {
			writeJSON(w, st)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /api/descendants", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if d := sv.Descendants(q.Get("project"), q.Get("path"), q.Get("sha")); d != nil {
			writeJSON(w, d)
			return
		}
		http.NotFound(w, r)
	})
	// kton-web's graph form, for its own viewer (?graph=), and the bytes behind its file preview
	mux.HandleFunc("GET /kton/graph.json", func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		writeJSON(w, sv.KtonGraph(project, func(path, sum string) string { return BlobURL("/kton/blob", project, path, sum) }))
	})
	mux.HandleFunc("GET /kton/blob", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		project, path, sum := q.Get("project"), q.Get("path"), q.Get("sha")
		if !sv.KnownFile(project, path, sum) {
			http.Error(w, "not an observed file with these bytes", http.StatusNotFound)
			return
		}
		// read once, hash what was read, serve that: a check by size and mtime followed by a
		// second open could serve other bytes under this hash (review B1)
		b, err := readAtMost(path, maxBlob)
		if err == errTooLarge {
			http.Error(w, "too large to preview", http.StatusRequestEntityTooLarge)
			return
		}
		if h := sha256.Sum256(b); err != nil || hex.EncodeToString(h[:]) != strings.TrimPrefix(sum, "sha256:") {
			http.Error(w, "not an observed file with these bytes", http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, filepath.Base(path), time.Time{}, bytes.NewReader(b))
	})
	if sv.Web != "" { // a kton-web checkout, served as it is (cockpit show --web does the same)
		mux.Handle("GET /web/", http.StripPrefix("/web/", http.FileServer(http.Dir(sv.Web))))
	}
	// the drafts of descriptions and rationales: GET ?prefix=<session>/ (repeatable), PUT {key, description, rationale, by}
	mux.HandleFunc("GET /api/notes", func(w http.ResponseWriter, r *http.Request) {
		if sv.Notes == nil {
			writeJSON(w, map[string]Note{})
			return
		}
		writeJSON(w, sv.Notes.All(r.URL.Query()["prefix"]...))
	})
	mux.HandleFunc("PUT /api/notes", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Key string `json:"key"`
			Note
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil || in.Key == "" || sv.Notes == nil {
			http.Error(w, "want {key, description, rationale, by}", http.StatusBadRequest)
			return
		}
		if in.By == "" {
			in.By = "you"
		}
		if err := sv.Notes.Put(in.Key, in.Note); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	// what is recorded and what is kept (watched.go): GET; POST {folder, on}; POST /api/keep {id, on};
	// DELETE /api/sessions/{id} (an ended session, its log and observations)
	mux.HandleFunc("GET /api/watched", func(w http.ResponseWriter, r *http.Request) {
		if sv.Watched == nil {
			writeJSON(w, WatchedState{})
			return
		}
		writeJSON(w, sv.Watched.State())
	})
	mux.HandleFunc("POST /api/watched", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Folder string `json:"folder"`
			On     bool   `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || !filepath.IsAbs(in.Folder) || sv.Watched == nil {
			http.Error(w, "want {folder: an absolute path, on}", http.StatusBadRequest)
			return
		}
		if err := sv.Watched.SetFolder(in.Folder, in.On); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, sv.Watched.State())
	})
	mux.HandleFunc("POST /api/keep", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
			On bool   `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || in.ID == "" || sv.Watched == nil {
			http.Error(w, "want {id, on}", http.StatusBadRequest)
			return
		}
		if !sv.With(in.ID, func(*Session) {}) {
			http.Error(w, "no session "+in.ID, http.StatusNotFound)
			return
		}
		if err := sv.Watched.SetKeep(in.ID, in.On); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := sv.DeleteSession(r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	// the editor windows: events the MCP tools post, and the windows' answers (ui.go)
	mux.HandleFunc("GET /api/ui/events", func(w http.ResponseWriter, r *http.Request) {
		if sv.UI == nil {
			http.Error(w, "no ui hub", http.StatusServiceUnavailable)
			return
		}
		sv.UI.serveEvents(w, r)
	})
	mux.HandleFunc("POST /api/ui/answer", func(w http.ResponseWriter, r *http.Request) {
		if sv.UI == nil {
			http.Error(w, "no ui hub", http.StatusServiceUnavailable)
			return
		}
		sv.UI.serveAnswer(w, r)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"web": sv.Web != ""})
	})
	// one tool call of a task as its own `cockpit publish` (ADR-010: a reference record per nmfe call)
	mux.HandleFunc("GET /api/calldraft", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pid, _ := strconv.Atoi(q.Get("pid"))
		var out *Draft
		ok := sv.With(q.Get("session"), func(s *Session) {
			for _, t := range s.Tasks {
				if t.ID != q.Get("task") {
					continue
				}
				for _, c := range sv.callsOf(s.ID, t) {
					if c.PID == pid {
						d := CallDraft(c, RepoRoot(s.Project), check.State)
						out = &d
					}
				}
			}
		})
		if !ok || out == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/draft", func(w http.ResponseWriter, r *http.Request) {
		session, task := r.URL.Query().Get("session"), r.URL.Query().Get("task")
		if session == DeclaredSession { // a declared step: its picked files, checked against their bytes now
			if sv.Edits != nil {
				if d, ok := sv.Edits.DeclaredByID(task); ok {
					writeJSON(w, DeclaredDraft(d))
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		var out *Draft
		ok := sv.With(session, func(s *Session) {
			for _, t := range s.Tasks {
				if t.ID == task {
					d := MakeDraft(s, sv.annotated(check, s, []*Task{t})[0])
					out = &d
				}
			}
		})
		if !ok || out == nil {
			http.NotFound(w, r)
			return
		}
		// read through the person's corrections (the story editor): every reader sees the corrected step
		if sv.Edits != nil {
			if ed, ok := sv.Edits.Step(session + "/" + task); ok {
				d := ApplyEdit(*out, ed)
				out = &d
			}
		}
		writeJSON(w, out)
	})
	// the story editor (edits.go): GET ?project=; PUT {key, inputs?, outputs?, cmd?} (nothing set: back to the
	// recording); POST /api/declared {project, declared?, inputs, outputs, cmd}; DELETE /api/declared?id=
	mux.HandleFunc("GET /api/edits", func(w http.ResponseWriter, r *http.Request) {
		if sv.Edits == nil {
			writeJSON(w, map[string]any{"steps": map[string]StepEdit{}, "declared": []DeclaredStep{}})
			return
		}
		project := r.URL.Query().Get("project")
		declared := sv.Edits.Declared(project)
		if declared == nil {
			declared = []DeclaredStep{}
		}
		writeJSON(w, map[string]any{"steps": sv.Edits.Steps(r.URL.Query()["prefix"]...), "declared": declared})
	})
	mux.HandleFunc("PUT /api/edits", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Key string `json:"key"`
			StepEdit
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil || in.Key == "" || sv.Edits == nil {
			http.Error(w, "want {key, inputs?, outputs?, cmd?}", http.StatusBadRequest)
			return
		}
		if in.By == "" {
			in.By = "you"
		}
		if err := sv.Edits.PutStep(in.Key, in.StepEdit); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/declared", func(w http.ResponseWriter, r *http.Request) {
		var in DeclareRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil || sv.Edits == nil {
			http.Error(w, "want {project, declared?, inputs, outputs, cmd}", http.StatusBadRequest)
			return
		}
		if !sv.isWatched(in.Project) {
			http.Error(w, "not a recorded folder: "+in.Project, http.StatusBadRequest)
			return
		}
		d, err := sv.Edits.Declare(in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, d)
	})
	// workspace heuristics (heuristics.go): GET; PUT {name, path} registers (empty path removes);
	// POST /api/heuristics/run {project, name} → proposals, checked, never stored (the person keeps one as a declared step)
	mux.HandleFunc("GET /api/heuristics", func(w http.ResponseWriter, r *http.Request) {
		if sv.Heuristics == nil {
			writeJSON(w, map[string]string{})
			return
		}
		writeJSON(w, sv.Heuristics.All())
	})
	mux.HandleFunc("PUT /api/heuristics", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Path string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || sv.Heuristics == nil {
			http.Error(w, "want {name, path}", http.StatusBadRequest)
			return
		}
		if err := sv.Heuristics.Register(in.Name, in.Path); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/heuristics/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Project, Name string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || sv.Heuristics == nil {
			http.Error(w, "want {project, name}", http.StatusBadRequest)
			return
		}
		if !sv.isWatched(in.Project) {
			http.Error(w, "not a recorded folder: "+in.Project, http.StatusBadRequest)
			return
		}
		props, err := sv.Heuristics.Run(r.Context(), in.Name, RepoRoot(in.Project))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if props == nil {
			props = []HeuristicProposal{}
		}
		writeJSON(w, props)
	})
	// an R value the person saved from the story (values.go): POST {hash, name, path, project}
	mux.HandleFunc("POST /api/values/saved", func(w http.ResponseWriter, r *http.Request) {
		var in SavedValue
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil || sv.Values == nil {
			http.Error(w, "want {hash, name, path, project}", http.StatusBadRequest)
			return
		}
		if !sv.isWatched(in.Project) {
			http.Error(w, "not a recorded folder: "+in.Project, http.StatusBadRequest)
			return
		}
		if err := sv.Values.MarkSaved(in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/declared", func(w http.ResponseWriter, r *http.Request) {
		if sv.Edits == nil {
			http.NotFound(w, r)
			return
		}
		if err := sv.Edits.Undeclare(r.URL.Query().Get("id")); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/lineage", func(w http.ResponseWriter, r *http.Request) {
		var out *Step
		ok := sv.With(r.URL.Query().Get("session"), func(s *Session) { out = sv.lineageAcross(s.ID, r.URL.Query().Get("file")) })
		if !ok || out == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, out)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// annotated: copies with the disk state of each file and links into other sessions.
func (sv *Service) annotated(check *Checker, s *Session, tasks []*Task) []*Task {
	v := check.Annotate(tasks)
	sv.linkAcross(s, v)
	return v
}
