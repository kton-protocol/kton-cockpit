// kton-call stands in for a tool (nmfe75, PsN's execute and vpc, bbi …): installed under the
// tool's name in a directory ahead of the real one on PATH, it is that tool.
//
// It treats the call as a function. Earlier calls with the same command line (ktonwatch knows
// them) say which files such a call reads; kton-call hashes today's files at those places and
// forms the call's key. When a recorded call has that key, its outputs are kept in the CAS, and
// nothing it read outside the project or looked for in vain has changed (watch.ReuseBlocked),
// the outputs are put in place and nothing is computed. Otherwise the real tool runs, exactly as
// if kton-call were not there.
//
// A Go program is invisible to the recorder's shim, so a reuse writes its own lines into the
// log ($KTON_TRACE_LOG): the call, the inputs it checked, the outputs it put in place.
//
//	KTON_WATCH   ktonwatch's address (default http://127.0.0.1:8378)
//	KTON_CAS     the CAS directory (default ~/.cache/kton-cockpit/cas)
//	KTON_CALLS   a JSON file of recorded calls (oldest first) to use instead of asking ktonwatch:
//	             inside a reproduction container, which has no network
//	KTON_REUSE=0 never reuse: always run the real tool
//	KTON_CALL_DEBUG=<file>  write why a call was or was not reused into this file
//	KTON_NO_REUSE=<key>  never reuse this one call (the one a reproduction is about)
//	KTON_REAL_<TOOL>  the real tool, e.g. KTON_REAL_NMFE75=/opt/nm751/run/nmfe75 (default: the
//	             next one of that name on PATH)
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

func main() {
	tool := filepath.Base(os.Args[0])
	if tool == "kton-call" {
		fmt.Fprintln(os.Stderr, "kton-call: install it under a tool's name (nmfe75, execute, vpc, bbi …), see recorder/install.sh")
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	argv := toolcall.NormArgv(append([]string{tool}, os.Args[1:]...), cwd)
	if os.Getenv("KTON_REUSE") != "0" {
		if c, ok := reusable(argv, cwd); ok {
			if err := putOutputs(c, cwd); err == nil {
				fmt.Fprintf(os.Stderr, "kton: %s from the store (key %s…, computed in command %s)\n", c.Tool, short(c.Key), c.Task)
				os.Exit(0)
			} else {
				fmt.Fprintf(os.Stderr, "kton: reuse abandoned, %v; computing\n", err)
			}
		}
	}
	real := realTool(tool)
	if real == "" {
		fmt.Fprintf(os.Stderr, "kton: %s is not installed here, and no kept call matches\n", tool)
		os.Exit(127)
	}
	err := execTool(real, append([]string{real}, os.Args[1:]...), os.Environ())
	fmt.Fprintf(os.Stderr, "kton: %s: %v\n", real, err)
	os.Exit(126)
}

func short(k string) string {
	k = strings.TrimPrefix(k, "sha256:")
	if len(k) > 12 {
		return k[:12]
	}
	return k
}

func casDir() string {
	if d := os.Getenv("KTON_CAS"); d != "" {
		return d
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".cache")
	}
	return filepath.Join(base, "kton-cockpit", "cas")
}

// reusable: the newest recorded call with this command line whose inputs, read today at the
// same places, give the same key, and whose outputs are all kept.
func debugf(format string, a ...any) {
	f := os.Getenv("KTON_CALL_DEBUG")
	if f == "" {
		return
	}
	if w, err := os.OpenFile(f, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(w, "%s [%d] "+format+"\n", append([]any{time.Now().Format("15:04:05"), os.Getpid()}, a...)...)
		w.Close()
	}
}

func reusable(argv []string, cwd string) (watch.Call, bool) {
	calls, ok := known(argv)
	debugf("cwd %s argv %q: %d earlier calls (query ok=%v)", cwd, argv, len(calls), ok)
	if !ok {
		return watch.Call{}, false
	}
	cas := &toolcall.CAS{Dir: casDir()}
	hashes := map[string]string{}
	for _, c := range calls {
		if len(c.Writes) == 0 || c.Key == os.Getenv("KTON_NO_REUSE") {
			continue // nothing to put in place, or the call under test
		}
		inputs := map[string]string{}
		ok := true
		for _, r := range c.Reads {
			h, seen := hashes[r.Rel]
			if !seen {
				h = sha256File(filepath.Join(cwd, r.Rel))
				hashes[r.Rel] = h
			}
			if h == "" {
				debugf("  %s/%s: %s is missing here", c.Session, c.Task, r.Rel)
				ok = false
				break
			}
			if h != r.SHA256 {
				debugf("  %s/%s: %s has other bytes (%.12s instead of %.12s)", c.Session, c.Task, r.Rel, h, r.SHA256)
			}
			inputs[r.Rel] = h
		}
		if !ok || watch.CallKey(argv, inputs) != c.Key {
			continue
		}
		if why := watch.ReuseBlocked(c, cwd, sha256File); why != "" {
			debugf("  %s/%s: %s", c.Session, c.Task, why)
			continue
		}
		for _, w := range c.Writes {
			if !cas.Has(w.SHA256) {
				debugf("  %s/%s: output %s not in the store", c.Session, c.Task, w.Rel)
				ok = false
				break
			}
		}
		if ok {
			debugf("  Treffer: %s/%s", c.Session, c.Task)
			return c, true
		}
	}
	return watch.Call{}, false
}

// known: recorded calls with this command line, newest first, from $KTON_CALLS or ktonwatch.
func known(argv []string) ([]watch.Call, bool) {
	want := strings.Join(argv, "\x00")
	if f := os.Getenv("KTON_CALLS"); f != "" {
		b, err := os.ReadFile(f)
		var all []watch.Call
		if err != nil || json.Unmarshal(b, &all) != nil {
			return nil, false
		}
		var out []watch.Call
		for i := len(all) - 1; i >= 0; i-- {
			if strings.Join(all[i].Argv, "\x00") == want {
				out = append(out, all[i])
			}
		}
		return out, true
	}
	base := os.Getenv("KTON_WATCH")
	if base == "" {
		base = "http://127.0.0.1:8378"
	}
	b, _ := json.Marshal(argv)
	cl := http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Get(strings.TrimRight(base, "/") + "/api/calls?argv=" + url.QueryEscape(string(b)))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var calls []watch.Call
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&calls) != nil {
		return nil, false
	}
	return calls, true
}

func sha256File(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// putOutputs copies the call's outputs from the CAS into place and writes the reuse into the
// log, so the recording shows this call like any other.
func putOutputs(c watch.Call, cwd string) error {
	cas := &toolcall.CAS{Dir: casDir()}
	type done struct{ path, sum string }
	var put []done
	for _, w := range c.Writes {
		dst := filepath.Join(cwd, w.Rel)
		if sha256File(dst) == strings.TrimPrefix(w.SHA256, "sha256:") {
			put = append(put, done{dst, w.SHA256})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFile(cas.Path(w.SHA256), dst); err != nil {
			return err
		}
		put = append(put, done{dst, w.SHA256})
	}
	logf := os.Getenv("KTON_TRACE_LOG")
	if logf == "" {
		return nil
	}
	f, err := os.OpenFile(logf, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil // no log, no record; the outputs are in place
	}
	defer f.Close()
	pid, ppid := os.Getpid(), os.Getppid()
	line := func(op, mode, path, extra string) {
		fmt.Fprintf(f, "%d\t%d\t%d\t%s\t%s\t%s\t0\t0\t0\t%s\n", time.Now().UnixNano(), pid, ppid, op, mode,
			watch.Escape(path), watch.Escape(extra))
	}
	line("exec", "q", cwd, toolcall.ShellJoin(append([]string{filepath.Base(os.Args[0])}, os.Args[1:]...))) // as the shim writes it
	for _, r := range c.Reads {
		line("open", "r", filepath.Join(cwd, r.Rel), "sha256="+r.SHA256)
	}
	for _, o := range c.Outside {
		line("open", "r", o.Path, "sha256="+o.SHA256) // checked unchanged by ReuseBlocked
	}
	for _, m := range c.Missing {
		line("openfail", "r", filepath.Join(cwd, m.Rel), "errno=2") // checked still absent
	}
	for _, p := range put {
		line("open", "w", p.path, "new=1")
		line("closew", "w", p.path, "sha256="+p.sum)
	}
	line("reuse", "-", cwd, "key="+c.Key+";from="+c.Session+"/"+c.Task)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".kton-tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// realTool: KTON_REAL_<TOOL>, else the next program of that name on PATH that is not this one.
func realTool(tool string) string {
	key := "KTON_REAL_" + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, tool)
	if p := os.Getenv(key); p != "" {
		return p
	}
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, tool)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			continue
		}
		if r, err := filepath.EvalSymlinks(p); err == nil && r == self {
			continue
		}
		return p
	}
	return ""
}
