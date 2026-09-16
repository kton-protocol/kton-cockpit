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
// cockpit has never known the store layout, so it cannot get that wrong — it asks the kernels for
// their records and forwards what comes back.
package show

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// Server answers the three files a viewer fetches.
//
// It used to start `kton serve` for both substrates and read /sync over HTTP. #83 removed that,
// noting that the cockpit was launching a server on a free local port to talk to itself — HTTP as a
// worse CLI — and #85 answered the same kton §12 query over stdout. So this is now two subprocess
// calls where it was two servers, two ports and a readiness poll, and the property that mattered is
// unchanged: the records come from the kernels, not from the store.
type Server struct {
	cfg *config.Config
	r   *binaries.Runner
}

// New returns a server over this repo's registries. Nothing is started: each request reads the
// registries as they are, so a reload shows what is there now rather than a snapshot from start-up.
func New(cfg *config.Config) *Server {
	return &Server{cfg: cfg, r: binaries.New(cfg)}
}

// Snapshot returns the three files a viewer fetches, as bytes.
func Snapshot(ctx context.Context, cfg *config.Config) (union, keys, names []byte, err error) {
	s := New(cfg)
	recs, err := s.r.Records(ctx)
	if err != nil {
		return nil, nil, nil, err
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

// serveUnion answers with every record both registries hold. Read per request rather than cached:
// a viewer of a live working repo that showed a start-up snapshot would be quietly stale.
func (s *Server) serveUnion(w http.ResponseWriter, req *http.Request) {
	recs, err := s.r.Records(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if recs == nil {
		recs = []binaries.Record{}
	}
	writeJSON(w, recs)
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
