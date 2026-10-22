// Package oxigraph is the index kind "oxigraph" (ADR-012): one long-running Oxigraph per machine,
// started by the first cockpit that needs it, holding the RDF projection SPARQL queries run on.
//
// The daemon is derived state. Its store lives under the user cache directory, everything in it is
// rebuilt from the registries through the kernel's projections, and deleting it changes no answer:
// the next query projects again.
//
// This file is the daemon's life cycle, which needs no kernel; store.go is what goes into it.
package oxigraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// Schema is the layout of what the cockpit writes into the store (graph names, what urn:kton:index
// says). A daemon started under another schema is not used, and a store written under another is
// emptied before use: its graphs would be read wrongly. 2: records stored once (ADR-012 phase 3).
const Schema = 2

// Daemon is a reachable Oxigraph.
type Daemon struct {
	URL string // the SPARQL server root, e.g. http://127.0.0.1:40123
	// Dir is the index directory (lock, store), empty for one given by index.url.
	Dir string
}

// lock is what a started daemon leaves in the index directory, so the next cockpit finds it and
// can tell whether it is one it may use.
type lock struct {
	Container string `json:"container"`
	URL       string `json:"url"`
	Image     string `json:"image"`
	Schema    int    `json:"schema"`
}

// ErrMismatch says a running daemon was started from another image or schema. It is not used, and
// a caller answers without it.
var ErrMismatch = errors.New("the running index daemon was started from another image or store schema")

// Dir is the machine's index directory: <UserCacheDir>/kton-cockpit/index. COCKPIT_INDEX_DIR moves
// it, for tests that must not meet the machine's own daemon.
func Dir() (string, error) {
	if d := os.Getenv("COCKPIT_INDEX_DIR"); d != "" {
		return d, os.MkdirAll(d, 0o755)
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(root, "kton-cockpit", "index")
	return d, os.MkdirAll(d, 0o755)
}

// containerName is per index directory, so a test's daemon never replaces the machine's.
func containerName(dir string) string {
	h := sha256.Sum256([]byte(dir))
	return "kton-cockpit-index-" + hex.EncodeToString(h[:6])
}

// Ensure returns the daemon for cfg: the one index.url names, or the machine's, started if it is
// not running.
func Ensure(ctx context.Context, cfg *config.Config) (*Daemon, error) {
	if u := cfg.Raw.Index.URL; u != "" {
		d := &Daemon{URL: strings.TrimSuffix(u, "/")}
		if err := d.ping(ctx); err != nil {
			return nil, fmt.Errorf("index.url %s: %w", u, err)
		}
		// Not ours to empty: a store there under another schema is refused, not wiped.
		return d, d.schema(ctx, false)
	}
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	unlock, err := flock(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()

	image := strings.TrimPrefix(cfg.Raw.Query.Image, "oci://")
	if l, ok := readLock(dir); ok {
		d := &Daemon{URL: l.URL, Dir: dir}
		if d.ping(ctx) == nil {
			if l.Image != image || l.Schema != Schema {
				return nil, fmt.Errorf("%w (%s, schema %d; this configuration wants %s, schema %d): "+
					"cockpit index stop, then the next query starts the right one", ErrMismatch, l.Image, l.Schema, image, Schema)
			}
			return d, d.schema(ctx, true)
		}
	}
	return start(ctx, cfg, dir, image)
}

// Running returns the machine's daemon if one is up, without starting one.
func Running(ctx context.Context) (*Daemon, *lock, error) {
	dir, err := Dir()
	if err != nil {
		return nil, nil, err
	}
	l, ok := readLock(dir)
	if !ok {
		return nil, nil, nil
	}
	d := &Daemon{URL: l.URL, Dir: dir}
	if d.ping(ctx) != nil {
		return nil, &l, nil
	}
	return d, &l, nil
}

func start(ctx context.Context, cfg *config.Config, dir, image string) (*Daemon, error) {
	if image == "" {
		return nil, fmt.Errorf("no Oxigraph image to start (query.image), and no index.url")
	}
	store := filepath.Join(dir, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		return nil, err
	}
	engine := cfg.Raw.Execution.EngineOrDefault()
	name := containerName(dir)
	_ = exec.CommandContext(ctx, engine, "rm", "-f", name).Run() // a stopped one of ours
	// Loopback only, no capabilities, as the user who owns the cache, with a memory ceiling so one
	// unbounded query cannot take the machine down.
	args := []string{"run", "-d", "--name", name,
		"--publish", "127.0.0.1::7878",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--memory", cfg.Raw.Index.MemoryOrDefault(),
		"--volume", store + ":/data",
		image, "serve", "--location", "/data", "--bind", "0.0.0.0:7878"}
	if out, err := exec.CommandContext(ctx, engine, args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("starting the index daemon: %v\n%s", err, bytes.TrimSpace(out))
	}
	out, err := exec.CommandContext(ctx, engine, "port", name, "7878/tcp").Output()
	if err != nil {
		return nil, fmt.Errorf("reading the index daemon's port: %w", err)
	}
	addr := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	d := &Daemon{URL: "http://" + addr, Dir: dir}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := d.ping(ctx); err == nil {
			break
		} else if time.Now().After(deadline) {
			return nil, fmt.Errorf("the index daemon did not answer within 30s: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := d.schema(ctx, true); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(lock{Container: name, URL: d.URL, Image: image, Schema: Schema}, "", "  ")
	return d, os.WriteFile(filepath.Join(dir, "daemon.json"), b, 0o644)
}

// Stop stops the machine's daemon. Its store stays; the next start serves it again.
func Stop(ctx context.Context, cfg *config.Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	unlock, err := flock(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	out, err := exec.CommandContext(ctx, cfg.Raw.Execution.EngineOrDefault(), "rm", "-f", containerName(dir)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stopping the index daemon: %v\n%s", err, bytes.TrimSpace(out))
	}
	if err := os.Remove(filepath.Join(dir, "daemon.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readLock(dir string) (lock, bool) {
	var l lock
	b, err := os.ReadFile(filepath.Join(dir, "daemon.json"))
	if err != nil || json.Unmarshal(b, &l) != nil || l.URL == "" {
		return l, false
	}
	return l, true
}

// schema makes sure the store is written under Schema. A store under another one (the views of
// schema 1, say) is emptied when wipe allows, and refused otherwise; an empty store is marked.
func (d *Daemon) schema(ctx context.Context, wipe bool) error {
	body, err := d.request(ctx, "/query", "text/csv", "query",
		`SELECT ?s (EXISTS { GRAPH ?g { ?a ?b ?c } } AS ?any) WHERE { OPTIONAL { GRAPH <urn:kton:index> { <urn:kton:index> <urn:kton:schema> ?s } } }`)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var have, any string
	if len(lines) == 2 {
		f := strings.Split(strings.TrimSpace(lines[1]), ",")
		have, any = f[0], f[len(f)-1]
	}
	if have == fmt.Sprint(Schema) {
		return nil
	}
	if any == "true" {
		if !wipe {
			return fmt.Errorf("%w: the store at %s was written under schema %q", ErrMismatch, d.URL, have)
		}
		if _, err := d.request(ctx, "/update", "", "update", "DROP ALL"); err != nil {
			return err
		}
	}
	_, err = d.request(ctx, "/update", "", "update",
		fmt.Sprintf(`INSERT DATA { GRAPH <urn:kton:index> { <urn:kton:index> <urn:kton:schema> "%d" } }`, Schema))
	return err
}

// Registries counts the registry directories the store holds records of.
func (d *Daemon) Registries(ctx context.Context) (int, error) {
	q := `SELECT (COUNT(DISTINCT ?r) AS ?n) WHERE { GRAPH <urn:kton:index> { ?r <urn:kton:position> ?p } }`
	body, err := d.request(ctx, "/query", "text/csv", "query", q)
	if err != nil {
		return 0, err
	}
	var n int
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) == 2 {
		fmt.Sscanf(strings.TrimSpace(lines[1]), "%d", &n)
	}
	return n, nil
}

// Clear empties the store. Nothing in it is authoritative: every registry is read again, at the
// next query that reads it.
func (d *Daemon) Clear(ctx context.Context) error {
	if _, err := d.request(ctx, "/update", "", "update", "DROP ALL"); err != nil {
		return err
	}
	return d.schema(ctx, true)
}

func (d *Daemon) request(ctx context.Context, path, accept, field, value string) ([]byte, error) {
	form := url.Values{field: {value}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// client bounds every request: a query that runs away is cut off, never waited for.
var client = &http.Client{Timeout: 2 * time.Minute}

func (d *Daemon) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL+"/", nil)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s answers %s", d.URL, resp.Status)
	}
	return nil
}
