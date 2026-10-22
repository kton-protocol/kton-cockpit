package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CAS keeps the files of tool calls by their content: <Dir>/sha256/<first two>/<hash>. A
// reproduction gets the inputs as they were then, not as they are now, and a call whose key
// has been seen before gets its outputs from here. Only inputs and outputs of calls are kept:
// what no call read and no call wrote is not needed to compute anything again.
type CAS struct {
	Dir     string
	MaxSize int64 // larger files are not kept (0: no limit)
}

// Path is where a hash is kept, whether or not it is.
func (c *CAS) Path(sum string) string {
	sum = strings.TrimPrefix(sum, "sha256:")
	if len(sum) < 3 {
		return filepath.Join(c.Dir, "sha256", "_", sum)
	}
	return filepath.Join(c.Dir, "sha256", sum[:2], sum)
}

// Has reports whether the bytes of this hash are kept.
func (c *CAS) Has(sum string) bool {
	_, err := os.Stat(c.Path(sum))
	return err == nil
}

// Put keeps the file at path under sum if its bytes still hash to sum. It reports whether the
// bytes are kept afterwards; a file that changed or is gone is not an error, only not kept.
func (c *CAS) Put(path, sum string) (bool, error) {
	sum = strings.TrimPrefix(sum, "sha256:")
	if len(sum) != 64 {
		return false, nil
	}
	if c.Has(sum) {
		return true, nil
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false, nil
	}
	if c.MaxSize > 0 && fi.Size() > c.MaxSize {
		return false, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return false, nil
	}
	defer in.Close()
	dst := c.Path(sum)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".put-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		tmp.Close()
		return false, fmt.Errorf("copy %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return false, nil // changed since it was observed: these bytes are gone
	}
	os.Chmod(tmp.Name(), 0o444)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return false, err
	}
	return true, nil
}

// pendingFile is a call's file waiting to be kept.
type pendingFile struct{ path, sum string }

// collectPending lists the files of finished tasks' calls not yet kept, and marks the tasks.
// Called with sv.mu held.
func (sv *Service) collectPending() []pendingFile {
	if sv.CAS == nil {
		for _, tr := range sv.sessions {
			tr.s.TakeSeen()
		}
		return nil
	}
	if sv.archived == nil {
		sv.archived = map[string]bool{}
	}
	var out []pendingFile
	seen := map[string]bool{}
	for _, tr := range sv.sessions {
		for _, o := range tr.s.TakeSeen() { // as the log shows them: before a tool deletes them
			if !seen[o.SHA256] {
				seen[o.SHA256] = true
				out = append(out, pendingFile{o.Path, o.SHA256})
			}
		}
	}
	for _, tr := range sv.sessions {
		for _, t := range tr.s.Tasks {
			k := tr.s.ID + "\x00" + t.ID
			if sv.archived[k] || (t.Status != "ok" && t.Status != "err") {
				continue // not yet finished: the tail can still grow
			}
			sv.archived[k] = true
			for _, c := range sv.callsOf(tr.s.ID, t) {
				for _, f := range append(append([]CallFile{}, c.Reads...), c.Writes...) {
					if f.SHA256 == "" || seen[f.SHA256] {
						continue
					}
					seen[f.SHA256] = true
					out = append(out, pendingFile{f.Path, f.SHA256})
				}
			}
		}
	}
	return out
}

// keep stores pending files; without the lock, since it reads files.
func (sv *Service) keep(files []pendingFile) {
	kept, missed := 0, 0
	for _, f := range files {
		if sv.CAS.Has(f.sum) {
			continue
		}
		ok, err := sv.CAS.Put(f.path, f.sum)
		switch {
		case err != nil:
			sv.logf("cas: %v", err)
		case ok:
			kept++
		default:
			missed++
		}
	}
	if kept+missed > 0 {
		sv.logf("cas: %d kept, %d changed or gone before they could be kept", kept, missed)
	}
}

func sha256Hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Producibility: for every file version a call of the project read or wrote, whether its bytes
// are kept, whether a recorded call can make them again from inputs that are kept or themselves
// producible, or whether they are lost. This is what a reproduction can still reach.
type Producible struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	State  string   `json:"state"`                 // kept | producible | lost
	Via    string   `json:"via,omitempty"`         // the call that makes it again: its tool and task
	Lost   []string `json:"lost_inputs,omitempty"` // why it is lost: inputs that are lost
}

func (sv *Service) ProjectProducible(project string) []Producible {
	has := func(string) bool { return false }
	if sv.CAS != nil {
		has = sv.CAS.Has
	}
	return producibleOf(sv.ProjectCalls(project), has)
}

func producibleOf(calls []Call, has func(sum string) bool) []Producible {
	type ver struct{ path, sum string }
	producers := map[string][]Call{}
	var order []ver
	seen := map[string]bool{}
	note := func(f CallFile) {
		if f.SHA256 != "" && !seen[f.SHA256] {
			seen[f.SHA256] = true
			order = append(order, ver{f.Path, f.SHA256})
		}
	}
	for _, c := range calls {
		for _, f := range c.Reads {
			note(f)
		}
		for _, f := range c.Writes {
			note(f)
			producers[f.SHA256] = append(producers[f.SHA256], c)
		}
	}
	state := map[string]Producible{}
	var resolve func(path, sum string, depth int) Producible
	resolve = func(path, sum string, depth int) Producible {
		if p, ok := state[sum]; ok {
			return p
		}
		p := Producible{Path: path, SHA256: sum, State: "lost"}
		switch {
		case has(sum):
			p.State = "kept"
		case depth > 64:
		default:
			state[sum] = p // a cycle counts as lost
			for _, c := range producers[sum] {
				var lost []string
				for _, in := range c.Reads {
					if r := resolve(in.Path, in.SHA256, depth+1); r.State == "lost" {
						lost = append(lost, in.Rel)
					}
				}
				if len(lost) == 0 {
					p.State, p.Via, p.Lost = "producible", c.Tool+" (command "+c.Task+")", nil
					break
				}
				if p.Lost == nil {
					p.Via, p.Lost = c.Tool+" (command "+c.Task+")", lost
				}
			}
		}
		state[sum] = p
		return p
	}
	out := make([]Producible, 0, len(order))
	for _, v := range order {
		out = append(out, resolve(v.path, v.sum, 0))
	}
	return out
}
