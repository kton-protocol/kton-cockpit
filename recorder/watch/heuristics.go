package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Workspace heuristics (MH 20.11.): work nobody recorded can still get its story when a script
// that knows the workspace reconstructs it, e.g. "a NONMEM run folder with run3.ctl and run3.lst
// is a run of nmfe on run3.ctl". The person registers the script once (an absolute path; nothing
// found in a repository is ever run by itself), runs it from a story, and checks each proposal in
// the editor; a kept one becomes a declared step, published as declared=heuristic:<name>.
//
// The contract: the script gets {"repo": <abs>, "files": [{"path", "size", "mtime_ns", "sha256"}]}
// on stdin (repository-relative paths, .kton/ and .git/ left out) and prints a JSON array of
// proposals {"inputs": [paths], "outputs": [paths], "cmd": "...", "note": "..."} on stdout,
// within a minute.

// HeuristicProposal is one step a heuristic proposes.
type HeuristicProposal struct {
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
	Cmd     string   `json:"cmd"`
	Note    string   `json:"note,omitempty"`
	// Refused: why the recorder does not offer it (a path that is not a file of the repository …)
	Refused string `json:"refused,omitempty"`
}

// Heuristics is the store of registered scripts, by name (heuristics.json).
type Heuristics struct {
	path string
	mu   sync.Mutex
	m    map[string]string // name → absolute path of the executable
	// Timeout for one run (a minute by default)
	Timeout time.Duration
}

var heuristicName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// OpenHeuristics reads the store under dir.
func OpenHeuristics(dir string) *Heuristics {
	h := &Heuristics{path: filepath.Join(dir, "heuristics.json"), m: map[string]string{}, Timeout: time.Minute}
	if b, err := os.ReadFile(h.path); err == nil {
		json.Unmarshal(b, &h.m)
	}
	return h
}

// All is every registered heuristic.
func (h *Heuristics) All() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]string{}
	for k, v := range h.m {
		out[k] = v
	}
	return out
}

// Register keeps a script under a name; an empty path removes it.
func (h *Heuristics) Register(name, path string) error {
	if !heuristicName.MatchString(name) {
		return fmt.Errorf("%q is not a name (letters, digits, . _ -)", name)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if path == "" {
		delete(h.m, name)
	} else {
		if !filepath.IsAbs(path) {
			return errors.New("the script is an absolute path")
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			return fmt.Errorf("%s is not an executable file", path)
		}
		h.m[name] = path
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(h.m, "", " ")
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

type heuristicFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime_ns"`
	SHA256 string `json:"sha256,omitempty"` // left out above 64 MB
}

// Run runs a registered heuristic over the repository and checks what it proposes.
func (h *Heuristics) Run(ctx context.Context, name, repo string) ([]HeuristicProposal, error) {
	h.mu.Lock()
	script, ok := h.m[name]
	h.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no heuristic %q is registered", name)
	}
	files, err := repoFiles(repo)
	if err != nil {
		return nil, err
	}
	in, _ := json.Marshal(map[string]any{"repo": repo, "files": files})
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script)
	cmd.Dir = repo
	cmd.Stdin = bytes.NewReader(in)
	// not recorded: the heuristic reads the workspace, it does not work in it
	cmd.Env = append(withoutTrace(os.Environ()), "KTON_HEURISTIC=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.WaitDelay = time.Second // a child that keeps the pipes open does not hold the run past its time
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return nil, fmt.Errorf("heuristic %s: %v %s", name, err, msg)
	}
	var props []HeuristicProposal
	if err := json.Unmarshal(out.Bytes(), &props); err != nil {
		return nil, fmt.Errorf("heuristic %s did not print a JSON array of proposals: %v", name, err)
	}
	have := map[string]bool{}
	for _, f := range files {
		have[f.Path] = true
	}
	for i := range props {
		p := &props[i]
		p.Cmd = strings.TrimSpace(p.Cmd)
		check := func(ps []string) string {
			for _, x := range ps {
				if err := plainRel(x); err != nil {
					return err.Error()
				}
				if !have[x] {
					return x + " is not a file of the repository"
				}
			}
			return ""
		}
		switch {
		case len(p.Outputs) == 0:
			p.Refused = "names no output"
		case p.Cmd == "":
			p.Refused = "says nothing about what was done (cmd)"
		default:
			if why := check(p.Inputs); why != "" {
				p.Refused = why
			} else if why := check(p.Outputs); why != "" {
				p.Refused = why
			}
		}
	}
	return props, nil
}

func withoutTrace(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		if strings.HasPrefix(e, "LD_PRELOAD=") || strings.HasPrefix(e, "KTON_TRACE_LOG=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// repoFiles: the repository's files, without .kton/, .git/ and other hidden folders.
func repoFiles(repo string) ([]heuristicFile, error) {
	var out []heuristicFile
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != repo && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(repo, p)
		f := heuristicFile{Path: filepath.ToSlash(r), Size: info.Size(), MTime: info.ModTime().UnixNano()}
		if info.Size() <= 64<<20 {
			f.SHA256, _ = hashOf(p)
		}
		out = append(out, f)
		if len(out) > 200000 {
			return errors.New("more than 200000 files: not a workspace a heuristic reads whole")
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}
