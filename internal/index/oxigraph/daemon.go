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
	"github.com/kton-protocol/kton-cockpit/internal/flock"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// Memory is the limit it was started with; empty in a lock written before it was recorded.
	Memory string `json:"memory,omitempty"`
	// MemoryFrom says where that limit came from: the index directory's memory file, the starting
	// repository's index.memory, or the default.
	MemoryFrom string `json:"memoryFrom,omitempty"`
}

// startWait is how long a started daemon has to answer.
const startWait = 3 * time.Minute

// MemoryFile is the floor every start of the daemon in an index directory takes: one line, a limit in
// the engine's notation (4g, 512m). A repository's index.memory above it still raises it.
const MemoryFile = "memory"

// Wanted is the memory a start of the daemon in dir asks for under cfg, where it comes from, and what
// is wrong with the directory's memory file, if anything (then the file is not used).
func Wanted(cfg *config.Config, dir string) (limit, from, problem string) {
	limit, from = cfg.Raw.Index.MemoryOrDefault(), "default"
	if cfg.Raw.Index.Memory != "" {
		from = "repository"
	}
	b, err := os.ReadFile(filepath.Join(dir, MemoryFile))
	if errors.Is(err, os.ErrNotExist) {
		return limit, from, ""
	}
	path := filepath.Join(dir, MemoryFile)
	if err != nil {
		return limit, from, fmt.Sprintf("%s cannot be read (%v): not used", path, err)
	}
	floor := strings.TrimSpace(string(b))
	if _, ok := config.MemoryBytes(floor); !ok {
		return limit, from, fmt.Sprintf("%s says %q, which is no limit (like 4g or 512m): not used", path, floor)
	}
	if !config.MemoryAbove(limit, floor) {
		return floor, path, ""
	}
	return limit, from, ""
}

// SharedMemory is what the daemon of the machine's index directory runs with and where that came from,
// and what is wrong with the directory's memory file; running is "" when no daemon was started there.
func SharedMemory(cfg *config.Config) (running, from, problem string) {
	dir, err := Dir()
	if err != nil {
		return "", "", err.Error()
	}
	_, _, problem = Wanted(cfg, dir)
	if l, ok := readLock(dir); ok {
		running, from = l.MemoryOrDefault(), l.MemoryFrom
	}
	return running, from, problem
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

// SharedIndex says whether cfg's index is the machine's shared daemon: the index directory is the
// machine's (however it is named: COCKPIT_INDEX_DIR may name the same directory), or index.url is the
// address that directory's daemon runs at (kton-review S1 on #215). A directory shared on purpose under
// another name is not the machine's, and not bound by its limit.
func SharedIndex(cfg *config.Config) bool {
	def, err := machineDir()
	if err != nil {
		return false
	}
	if u := cfg.Raw.Index.URL; u != "" {
		l, ok := readLock(def)
		return ok && strings.TrimSuffix(l.URL, "/") == strings.TrimSuffix(u, "/")
	}
	dir, err := Dir()
	return err == nil && sameDir(dir, def)
}

// machineDir is the machine's index directory, whatever COCKPIT_INDEX_DIR says.
func machineDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "kton-cockpit", "index"), nil
}

func sameDir(a, b string) bool {
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
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
		// The bookkeeping a started daemon keeps beside its store (which view is built under which key,
		// the locks that let two cockpits take turns) is kept for this URL too, in a directory of its
		// own. Without it every query rebuilt the view: 25-37 s per ask where the engine answers in
		// 2 ms (qm-dev-provenance, 20.11.). A kept view is trusted only once the store confirms it
		// (viewIs), so a store that changed under another writer is rebuilt as before.
		if dir, err := Dir(); err == nil {
			h := sha256.Sum256([]byte(d.URL))
			d.Dir = filepath.Join(dir, "url-"+hex.EncodeToString(h[:8]))
		}
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
	unlock, err := flock.Lock(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()

	image := strings.TrimPrefix(cfg.Raw.Query.Image, "oci://")
	if l, ok := readLock(dir); ok {
		d := &Daemon{URL: l.URL, Dir: dir}
		if d.ping(ctx) != nil && relocate(ctx, cfg, dir, &l) {
			d.URL = l.URL // the engine restarted it, perhaps on another port
		}
		if d.ping(ctx) == nil {
			if l.Image != image || l.Schema != Schema {
				return nil, fmt.Errorf("%w (%s, schema %d; this configuration wants %s, schema %d): "+
					"cockpit index stop, then the next query starts the right one", ErrMismatch, l.Image, l.Schema, image, Schema)
			}
			// A configuration that asks for more memory than the daemon was started with restarts
			// it on the same store; one that asks for less leaves it, so two configurations never
			// take turns restarting it.
			if want, _, _ := Wanted(cfg, dir); !config.MemoryAbove(want, l.MemoryOrDefault()) {
				return d, d.schema(ctx, true)
			}
		}
	}
	return start(ctx, cfg, dir, image)
}

// relocate finds a daemon the engine restarted by itself (after a crash or a reboot: it runs with
// --restart unless-stopped) at the port it has now, which the engine chose anew, and records it in
// the lock. It reports whether the daemon answers there.
func relocate(ctx context.Context, cfg *config.Config, dir string, l *lock) bool {
	out, err := exec.CommandContext(ctx, cfg.Raw.Execution.EngineOrDefault(), "port", l.Container, "7878/tcp").Output()
	if err != nil {
		return false
	}
	addr := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if addr == "" {
		return false
	}
	// Just restarted, it may not answer yet: a few seconds, not start's 30.
	d := &Daemon{URL: "http://" + addr, Dir: dir}
	for deadline := time.Now().Add(5 * time.Second); d.ping(ctx) != nil; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return false
		}
	}
	if d.URL == l.URL {
		return true
	}
	l.URL = d.URL
	b, _ := json.MarshalIndent(l, "", "  ")
	return os.WriteFile(filepath.Join(dir, "daemon.json"), b, 0o644) == nil
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

// Up returns the daemon cfg would use if it answers now, never starting, restarting or writing to
// one: index.url's, or the machine's when it runs the image and schema this configuration wants.
// Nil when there is none; a reader then has no view to read, which is what it would have had.
func Up(ctx context.Context, cfg *config.Config) *Daemon {
	if u := cfg.Raw.Index.URL; u != "" {
		d := &Daemon{URL: strings.TrimSuffix(u, "/")}
		if dir, err := Dir(); err == nil {
			h := sha256.Sum256([]byte(d.URL))
			d.Dir = filepath.Join(dir, "url-"+hex.EncodeToString(h[:8]))
		}
		if d.ping(ctx) != nil {
			return nil
		}
		return d
	}
	d, l, err := Running(ctx)
	if err != nil || d == nil || l == nil {
		return nil
	}
	if l.Image != strings.TrimPrefix(cfg.Raw.Query.Image, "oci://") || l.Schema != Schema {
		return nil
	}
	return d
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
	memory, memoryFrom, _ := Wanted(cfg, dir)
	_ = exec.CommandContext(ctx, engine, "rm", "-f", name).Run() // a stopped one of ours
	// Loopback only, no capabilities, as the user who owns the cache, with a memory ceiling so one
	// unbounded query cannot take the machine down. The engine brings it back after a crash or a
	// reboot (every lane's reads depend on it); stop removes it, so it stays stopped then.
	args := []string{"run", "-d", "--name", name, "--restart", "unless-stopped",
		"--publish", "127.0.0.1::7878",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--memory", memory,
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
	// A daemon opening a large store takes a while before it answers (userinterface, 21.11.: past 30 s for
	// a window federating twelve registries); a query waits for it rather than fail.
	deadline := time.Now().Add(startWait)
	for {
		if err := d.ping(ctx); err == nil {
			break
		} else if time.Now().After(deadline) {
			return nil, fmt.Errorf("the index daemon did not answer within %s (it may still be opening a large store: it keeps starting, and the next query finds it): %w", startWait, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := d.schema(ctx, true); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(lock{Container: name, URL: d.URL, Image: image, Schema: Schema, Memory: memory, MemoryFrom: memoryFrom}, "", "  ")
	return d, os.WriteFile(filepath.Join(dir, "daemon.json"), b, 0o644)
}

// Stop stops the machine's daemon. Its store stays; the next start serves it again.
func Stop(ctx context.Context, cfg *config.Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	unlock, err := flock.Lock(filepath.Join(dir, "daemon.lock"))
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

// MemoryOrDefault is the limit the daemon was started with; a lock that does not say was written
// when every daemon started with the default.
func (l lock) MemoryOrDefault() string {
	if l.Memory == "" {
		return config.Index{}.MemoryOrDefault()
	}
	return l.Memory
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
	// A store being started under this schema holds nothing a kept dataset names.
	d.forgetDatasets()
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
	d.forgetDatasets()
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

// MemoryInUse is how much of its memory limit the daemon of cfg's index directory uses now, in percent,
// and the limit it was started with; ok is false where there is no daemon of ours to ask (index.url, or
// none started).
func MemoryInUse(ctx context.Context, cfg *config.Config) (percent float64, limit string, ok bool) {
	if cfg.Raw.Index.URL != "" {
		return 0, "", false
	}
	dir, err := Dir()
	if err != nil {
		return 0, "", false
	}
	l, found := readLock(dir)
	if !found || l.Container == "" {
		return 0, "", false
	}
	out, err := exec.CommandContext(ctx, cfg.Raw.Execution.EngineOrDefault(), "stats", "--no-stream", "--format", "{{.MemPerc}}", l.Container).Output()
	if err != nil {
		return 0, "", false
	}
	p, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(string(out)), "%"), 64)
	if err != nil {
		return 0, "", false
	}
	return p, l.MemoryOrDefault(), true
}
