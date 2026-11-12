package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A cockpit repository in a mode an extension provides (repo.mode "improve") cannot be read by the
// cockpit core built into ktonwatch, which refuses a mode it does not know. Its records are served by
// that mode's own binary, cockpit-<mode> (~/.local/bin, else PATH), as `cockpit-<mode> show` on a
// loopback port, one per repository, started on the first request and proxied to. With improve the
// local registry is a cache of improve; the binary still asks for the person's improve sign-in, which
// the recorder extension keeps in $XDG_RUNTIME_DIR/kton/improve-access-token.json (memory only). A
// show older than extShowMaxAge is started again with the token of that moment.
const extShowMaxAge = 4 * time.Minute

type extShow struct {
	cmd     *exec.Cmd
	proxy   *httputil.ReverseProxy
	started time.Time
	done    chan struct{}
	stderr  *bytes.Buffer
}

type extShows struct {
	mu sync.Mutex
	m  map[string]*extShow // repo → its show
}

func newExtShows() *extShows { return &extShows{m: map[string]*extShow{}} }

func (e *extShows) serve(w http.ResponseWriter, r *http.Request, prefix, repo, mode string) {
	s, err := e.get(repo, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	http.StripPrefix(prefix, s.proxy).ServeHTTP(w, r)
}

func (e *extShows) get(repo, mode string) (*extShow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.m[repo]; s != nil {
		select {
		case <-s.done: // ended: start again
		default:
			if time.Since(s.started) < extShowMaxAge {
				return s, nil
			}
			_ = s.cmd.Process.Kill()
		}
		delete(e.m, repo)
	}
	s, err := startExtShow(repo, mode)
	if err != nil {
		return nil, err
	}
	e.m[repo] = s
	return s, nil
}

func startExtShow(repo, mode string) (*extShow, error) {
	exe := extBinary("cockpit-" + mode)
	if exe == "" {
		return nil, fmt.Errorf("this repository is in mode %q; its records need cockpit-%s, which is not installed (~/.local/bin or PATH)", mode, mode)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := l.Addr().String()
	l.Close()
	cmd := exec.Command(exe, "show", addr)
	cmd.Dir = repo
	cmd.Env = extShowEnv()
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	setParentDeath(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &extShow{cmd: cmd, started: time.Now(), done: make(chan struct{}), stderr: &stderr}
	go func() { _ = cmd.Wait(); close(s.done) }()
	u, _ := url.Parse("http://" + addr)
	s.proxy = httputil.NewSingleHostReverseProxy(u)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		select {
		case <-s.done:
			msg := strings.TrimSpace(stderr.String())
			if strings.Contains(msg, "no improve credentials") {
				msg = "not signed in to improve in Positron (the recorder extension keeps the sign-in for kton): sign in with the improve extension, allow kton, then open the graph again (" + msg + ")"
			}
			return nil, fmt.Errorf("cockpit-%s show: %s", mode, msg)
		default:
		}
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			return s, nil
		}
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("cockpit-%s show did not start in 20 s", mode)
}

// extShowEnv: ktonwatch's environment, with the person's improve access token when the recorder
// extension holds one and none is set already
func extShowEnv() []string {
	env := os.Environ()
	if os.Getenv("IMPROVE_TOKEN") != "" || os.Getenv("IMPROVE_TOKEN_FILE") != "" {
		return env
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", fmt.Sprint(os.Getuid()))
	}
	b, err := os.ReadFile(filepath.Join(dir, "kton", "improve-access-token.json"))
	if err != nil {
		return env
	}
	var t struct {
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(b, &t) == nil && t.AccessToken != "" {
		env = append(env, "IMPROVE_TOKEN="+t.AccessToken)
	}
	return env
}

func extBinary(name string) string {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// repoMode: repo.mode of the repository's cockpit.config.json ("" when unreadable)
func repoMode(repo string) string {
	b, err := os.ReadFile(filepath.Join(repo, "cockpit.config.json"))
	if err != nil {
		return ""
	}
	var c struct {
		Repo struct {
			Mode string `json:"mode"`
		} `json:"repo"`
	}
	if json.Unmarshal(b, &c) != nil {
		return ""
	}
	return c.Repo.Mode
}
