// cockpit-shim stands in for a tool (nmfe75, execute …) in a calling environment (ADR-014):
// installed under the tool's name ahead of it on PATH, it is that tool. It holds no key and no
// registry. It collects the call — its arguments as the key sees them, and the files it reads —
// hands it to the cockpit's dispatcher outside this environment, and puts back what comes back:
// the output files, stdout, stderr and the exit code. Whether the dispatcher answered from a record
// or ran the tool in its own environment, it says on stderr.
//
//	KTON_SHIM        where the dispatcher answers: unix:/path/to.sock or http://127.0.0.1:PORT
//	KTON_SHIM_TOKEN  a bearer token, when the listener asks for one (ktonwatch)
//	KTON_SHIM_COLLECTOR  how inputs are found: files or nonmem (default: nonmem for nmfe*, else files)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
)

type callFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

type toolCall struct {
	Tool      string     `json:"tool"`
	Args      []string   `json:"args"`
	Cwd       string     `json:"cwd"`
	Collector string     `json:"collector"`
	Files     []callFile `json:"files"`
}

type toolResult struct {
	Files    []callFile `json:"files"`
	Stdout   []byte     `json:"stdout"`
	Stderr   []byte     `json:"stderr"`
	ExitCode int        `json:"exitCode"`
	Note     string     `json:"note"`
	Code     string     `json:"code"`
	Reason   string     `json:"reason"`
}

func main() {
	tool := filepath.Base(os.Args[0])
	if tool == "cockpit-shim" {
		fmt.Fprintln(os.Stderr, "cockpit-shim: install it under a tool's name (nmfe75, execute …) ahead of the tool on PATH; see ADR-014")
		os.Exit(2)
	}
	code, err := run(tool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kton: %s: %v\n", tool, err)
		os.Exit(2)
	}
	os.Exit(code)
}

func run(tool string) (int, error) {
	endpoint := os.Getenv("KTON_SHIM")
	if endpoint == "" {
		return 0, fmt.Errorf("KTON_SHIM is not set: no dispatcher to hand the call to")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	root := toolcall.RepoRoot(cwd)
	rel, err := filepath.Rel(root, cwd)
	if err != nil {
		return 0, err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		rel = ""
	}
	collector := os.Getenv("KTON_SHIM_COLLECTOR")
	if collector == "" {
		collector = "files"
		if strings.HasPrefix(tool, "nmfe") {
			collector = "nonmem"
		}
	}
	args := toolcall.NormArgv(append([]string{tool}, os.Args[1:]...), cwd)[1:]
	dir := rel
	if dir == "" {
		dir = "."
	}
	paths, err := toolcall.Collect(collector, root, dir, args)
	if err != nil {
		return 0, err
	}
	call := toolCall{Tool: tool, Args: args, Cwd: rel, Collector: collector}
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return 0, err
		}
		call.Files = append(call.Files, callFile{Path: p, Data: b})
	}

	res, err := send(endpoint, call)
	if err != nil {
		return 0, err
	}
	for _, f := range res.Files {
		if err := writeBack(root, f); err != nil {
			return 0, err
		}
	}
	os.Stdout.Write(res.Stdout)
	os.Stderr.Write(res.Stderr)
	fmt.Fprintln(os.Stderr, res.Note)
	if res.ExitCode < 0 {
		return 1, nil
	}
	return res.ExitCode, nil
}

func send(endpoint string, call toolCall) (*toolResult, error) {
	body, err := json.Marshal(call)
	if err != nil {
		return nil, err
	}
	client, url := http.DefaultClient, endpoint+"/call"
	if sock, ok := strings.CutPrefix(endpoint, "unix:"); ok {
		client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
		url = "http://dispatcher/call"
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("KTON_SHIM_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the dispatcher at %s does not answer: %w", endpoint, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var res toolResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, fmt.Errorf("the dispatcher answered %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refused (%s): %s", res.Code, res.Reason)
	}
	return &res, nil
}

// writeBack puts one output in place under root. The path must be clean and relative, no directory
// on the way may be a link, and the file itself is not written through one (ADR-014, section 8, D2).
func writeBack(root string, f callFile) error {
	if err := toolcall.CheckRel(f.Path); err != nil {
		return fmt.Errorf("the dispatcher returned %v", err)
	}
	parts := strings.Split(f.Path, "/")
	dir := root
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		fi, err := os.Lstat(dir)
		switch {
		case os.IsNotExist(err):
			if err := os.Mkdir(dir, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return fmt.Errorf("%s is not a directory (a link?); %s is not written", dir, f.Path)
		}
	}
	w, err := os.OpenFile(filepath.Join(dir, parts[len(parts)-1]), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", f.Path, err)
	}
	if _, err := w.Write(f.Data); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
