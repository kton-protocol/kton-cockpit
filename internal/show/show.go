// Package show serves this repo's records to a kton-web viewer.
//
// It renders nothing itself and reads no registry files. The kernel's own charter puts the division
// here — `plankton export`'s source says "RENDERING (HTML, audio, UI) is a cockpit's job, not the
// kernel's (charter: the substrate stores and connects, it does not render)" — and this is the
// cockpit's side of that: fetch the records from the kernel, hand them to the viewer.
//
// The union is assembled without parsing a single store file, and that is deliberate rather than
// convenient. kton-web's own reader documents what parsing them wrongly costs: against a 2032-record
// corpus, a whole-file JSON.parse alone found 68 records and skipped 44 files "without a word", and
// the graph viewer drew a perfectly convincing lineage-only picture from it. Nothing errored. The
// cockpit has never known the store layout, so it cannot get that wrong — it asks `kton serve` for
// the records, over the same /sync endpoint a peer would use, and forwards what comes back.
package show

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// syncResp is what `kton serve`'s /sync returns. A record is `{seq, fotonId|claimId, envelope}`,
// which is exactly the shape kton-web's union reader accepts:
// `isRecord = (r) => !!(r && r.envelope && (r.fotonId || r.claimId))`. So the union is the two
// substrates' records concatenated, and nothing here has to understand what is inside them.
type syncResp struct {
	Records []json.RawMessage `json:"records"`
	Max     int               `json:"max"`
}

// Server holds the two kernel servers this one reads from.
type Server struct {
	cfg      *config.Config
	plankton string // base URL of the kernel's plankton federation API
	nekton   string
	procs    []*exec.Cmd
}

// Start launches `kton serve` for both substrates on free local ports. They are the only thing that
// touches the registry directories; this process never opens them.
func Start(ctx context.Context, cfg *config.Config) (*Server, error) {
	ktonBin := filepath.Join(cfg.BinDir, "kton")
	if _, err := os.Stat(ktonBin); err != nil {
		return nil, fmt.Errorf(
			"no kton binary at %s — show reads the records through `kton serve` rather than parsing the "+
				"registry itself. Build it from a kton checkout (see CLAUDE.md, \"The kernel binaries\")", ktonBin)
	}
	s := &Server{cfg: cfg}
	for _, sub := range []struct {
		name, dir string
		target    *string
	}{
		{"plankton", cfg.PlanktonDir, &s.plankton},
		{"nekton", cfg.NektonDir, &s.nekton},
	} {
		port, err := freePort()
		if err != nil {
			s.Stop()
			return nil, err
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		cmd := exec.CommandContext(ctx, ktonBin, "serve", sub.name, addr)
		cmd.Dir = cfg.RepoRoot
		cmd.Env = append(os.Environ(),
			"PLANKTON_DIR="+cfg.PlanktonDir,
			"NEKTON_DIR="+cfg.NektonDir,
		)
		cmd.Stdout, cmd.Stderr = io.Discard, os.Stderr
		if err := cmd.Start(); err != nil {
			s.Stop()
			return nil, fmt.Errorf("starting kton serve %s: %w", sub.name, err)
		}
		s.procs = append(s.procs, cmd)
		*sub.target = "http://" + addr
	}

	if err := s.waitReady(ctx); err != nil {
		s.Stop()
		return nil, err
	}
	return s, nil
}

// Snapshot returns the three files a viewer fetches, as bytes, by starting the kernel servers,
// reading them once and shutting them down again. It is what publishing a union to git uses: the
// same records the live server would hand a viewer, frozen at this moment.
func Snapshot(ctx context.Context, cfg *config.Config) (union, keys, names []byte, err error) {
	s, err := Start(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	defer s.Stop()

	var recs []json.RawMessage
	for _, base := range []string{s.plankton, s.nekton} {
		r, ferr := fetchRecords(ctx, base)
		if ferr != nil {
			return nil, nil, nil, ferr
		}
		recs = append(recs, r...)
	}
	if recs == nil {
		recs = []json.RawMessage{}
	}
	k, n, err := s.ring(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if union, err = json.Marshal(recs); err != nil {
		return nil, nil, nil, err
	}
	if keys, err = json.MarshalIndent(k, "", " "); err != nil {
		return nil, nil, nil, err
	}
	if names, err = json.MarshalIndent(n, "", " "); err != nil {
		return nil, nil, nil, err
	}
	return union, keys, names, nil
}

// WriteUnion regenerates this repo's published union under cfg's configured union directory and
// returns the repo-relative paths it wrote, for the caller to commit.
func WriteUnion(ctx context.Context, cfg *config.Config) ([]string, error) {
	union, keys, names, err := Snapshot(ctx, cfg)
	if err != nil {
		return nil, err
	}
	dir := cfg.Raw.Union.DirOrDefault()
	abs := filepath.Join(cfg.RepoRoot, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for name, b := range map[string][]byte{"union.json": union, "keys.json": keys, "names.json": names} {
		if err := os.WriteFile(filepath.Join(abs, name), append(b, '\n'), 0o644); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(filepath.Join(dir, name)))
	}
	sort.Strings(written) // a stable order, so a commit's file list does not churn between runs
	return written, nil
}

// Stop shuts the kernel servers down.
func (s *Server) Stop() {
	for _, p := range s.procs {
		if p.Process != nil {
			_ = p.Process.Kill()
		}
	}
}

func (s *Server) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(10 * time.Second)
	for _, base := range []string{s.plankton, s.nekton} {
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			resp, err := http.Get(base + "/healthz")
			if err == nil {
				resp.Body.Close()
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("kton serve at %s did not become reachable", base)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return nil
}

// Handler serves the three data files a viewer fetches, and — when webDir names a kton-web checkout
// — the kit itself behind them.
//
// webDir is optional on purpose: the data endpoints do not depend on it, and serving them alone is
// useful, since any viewer that takes ?union=&keys=&names= can be pointed at this server from
// wherever it is already running.
func (s *Server) Handler(webDir string) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/data/union.json", s.serveUnion)
	mux.HandleFunc("/data/keys.json", s.serveKeys)
	mux.HandleFunc("/data/names.json", s.serveNames)
	if webDir != "" {
		if _, err := os.Stat(filepath.Join(webDir, "runtime", "kton.js")); err != nil {
			return nil, fmt.Errorf(
				"%s does not look like a kton-web checkout (no runtime/kton.js) — pass the checkout with "+
					"--web or KTON_WEB, or omit it to serve only the data endpoints", webDir)
		}
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
	}
	return mux, nil
}

// serveUnion concatenates both substrates' records. Fetched per request rather than cached, so a
// reload shows what the registry holds now — a viewer of a live working repo that showed a snapshot
// from start-up would be quietly stale.
func (s *Server) serveUnion(w http.ResponseWriter, req *http.Request) {
	var all []json.RawMessage
	for _, base := range []string{s.plankton, s.nekton} {
		recs, err := fetchRecords(req.Context(), base)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		all = append(all, recs...)
	}
	if all == nil {
		all = []json.RawMessage{}
	}
	writeJSON(w, all)
}

func fetchRecords(ctx context.Context, base string) ([]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/sync", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading records from %s: %w", base, err)
	}
	defer resp.Body.Close()
	var sr syncResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("reading records from %s: %w", base, err)
	}
	return sr.Records, nil
}

// serveKeys maps keyid -> public key, which is what lets the viewer re-verify a signature instead
// of trusting a label.
//
// The keys are this repo's CONFIGURED TRUST TIERS, not every .pub lying in the registry. kton-web's
// own union builder takes the directory, which is right for a corpus someone hands you; here the
// config is the ceiling everywhere else — cockpit_ask includes a record only if a configured key
// verifies it — and a viewer that re-verified against a wider ring than the tool would show as
// trusted what ask correctly excludes.
func (s *Server) serveKeys(w http.ResponseWriter, req *http.Request) {
	keys, _, err := s.ring(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, keys)
}

// serveNames maps keyid -> the pubkey's filename. A name is not an identity — it is a label until
// something trustworthy binds it — so this is deliberately just what the file is called.
func (s *Server) serveNames(w http.ResponseWriter, req *http.Request) {
	_, names, err := s.ring(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, names)
}

// ring reads every configured trust-tier pubkey and keys it by keyid. The keyid comes from
// `plankton keyid`, not from a hash computed here: deriving an identifier the substrate also
// derives is exactly the reimplementation this cockpit does not do.
func (s *Server) ring(ctx context.Context) (keys, names map[string]string, err error) {
	r := binaries.New(s.cfg)
	keys, names = map[string]string{}, map[string]string{}
	for path, tier := range s.cfg.TierPubkeys() {
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, nil, fmt.Errorf("reading trusted pubkey %s: %w", path, rerr)
		}
		kid, kerr := r.KeyID(ctx, path)
		if kerr != nil {
			return nil, nil, kerr
		}
		keys[kid] = strings.TrimSpace(string(b))
		names[kid] = fmt.Sprintf("%s (%s)", strings.TrimSuffix(filepath.Base(path), ".pub"), tier)
	}
	return keys, names, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// freePort asks the kernel for an unused port and hands it on. There is a window between closing
// and `kton serve` binding; on a developer machine serving one repo that is acceptable, and a
// collision surfaces as a startup error rather than as wrong data.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
