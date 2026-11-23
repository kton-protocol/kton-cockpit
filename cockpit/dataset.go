package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/container"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// dataset.go is what a SPARQL query reads (oxigraph.Dataset) and how it is evaluated in a container
// started for it alone: for the vocabulary queries (query.go) and a query of one's own (sparql.go).

// inContainer evaluates q in a container started for it alone, over the dataset projected for keys,
// and returns the engine's answer in format. A timeout above zero bounds the evaluation.
func inContainer(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, q []byte, format, dataset string, timeout time.Duration) (string, []oxigraph.ClaimGraph, error) {
	return inContainerOver(ctx, cfg, keys, nil, nil, q, format, dataset, timeout)
}

// evalPin is what a query run evaluates with in place of the repository's present (ADR-020): the
// engine, the view's signers and vocabulary, and the registries each claim came from. The first run
// and every reproduction evaluate with the same pin, so the repository's state cannot make them differ.
type evalPin struct {
	image   string
	vc      oxigraph.ViewConfig
	sources map[string][]string
	loaded  []loadedBytes // each into the graph its hash names (ADR-017)
}

// inContainerOver is inContainer over union u, or over cfg's union as it is when u is nil, evaluated
// with pin, or with cfg's present when pin is nil.
func inContainerOver(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, u *binaries.Union, pin *evalPin, q []byte, format, dataset string, timeout time.Duration) (string, []oxigraph.ClaimGraph, error) {
	image := cfg.Raw.Query.Image
	if pin != nil {
		image = pin.image
	}
	// Under the checkout's own local directory, not the system's temporary one: the engine mounts it by
	// its path, and where the cockpit reaches the engine through a socket passed in from the host (the
	// persona windows), only a path on the host's side exists for it (userinterface, 21.11.). It is
	// never shared (ADR-016) and removed after the query.
	tmp := filepath.Join(cfg.LocalDir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", nil, refuse("io", "", "%v", err)
	}
	// What a killed query left (its projected dataset, large in a big union) is swept once it is an hour
	// old: the checkout is not cleared on reboot as the system's temporary directory is (kton-review N1).
	if old, err := filepath.Glob(filepath.Join(tmp, "query-*")); err == nil {
		for _, p := range old {
			if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > time.Hour {
				_ = os.RemoveAll(p)
			}
		}
	}
	dir, err := os.MkdirTemp(tmp, "query-")
	if err != nil {
		return "", nil, refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(dir)
	// Kept fresh while the query runs, so a long one is never taken for what a killed query left
	// (kton-review's note on #223's sweep).
	running := make(chan struct{})
	defer close(running)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-running:
				return
			case now := <-t.C:
				_ = os.Chtimes(dir, now, now)
			}
		}
	}()
	graphs, err := projectUnion(ctx, cfg, keys, u, pin, dataset, dir)
	if err != nil {
		return "", nil, refuse("kernel", "", "projecting the registries: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "query.rq"), q, 0o644); err != nil {
		return "", nil, refuse("io", "", "%v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "store"), 0o755); err != nil {
		return "", nil, refuse("io", "", "%v", err)
	}
	files := []string{"--file", "/q/default.nt", "--file", "/q/named.nq"}
	if dataset == oxigraph.DatasetClaims {
		files = []string{"--file", "/q/default.nt", "--file", "/q/claims.nt", "--file", "/q/scopes.nq"}
	}
	if _, err := container.RunTool(ctx, cfg, image, dir, append([]string{"load", "--location", "/q/store"}, files...)...); err != nil {
		return "", nil, refuse("query.failed", "", "%v", err)
	}
	if pin != nil && len(pin.loaded) > 0 {
		// Loaded under the query's own time bound, and counted against index.derived.maxTriples as a load is
		// (kton-review L1 on #235).
		lctx := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			lctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		var values []string
		for _, l := range pin.loaded {
			name := "loaded-" + l.hex
			if err := os.WriteFile(filepath.Join(dir, name), l.data, 0o644); err != nil {
				return "", nil, refuse("io", "", "%v", err)
			}
			g := oxigraph.GraphOf(l.hex)
			if _, err := container.RunTool(lctx, cfg, image, dir, "load", "--location", "/q/store", "--file", "/q/"+name,
				"--format", l.mediaType, "--graph", g, "--base", g); err != nil {
				if lctx.Err() == context.DeadlineExceeded {
					return "", nil, refuse("query.timeout", "ADR-020", "loading sha256:%s took longer than %s", l.hex, timeout)
				}
				return "", nil, refuse("query.failed", "ADR-020", "the loaded graph sha256:%s: %v", l.hex, err)
			}
			values = append(values, "<"+g+">")
		}
		count := "SELECT (COUNT(*) AS ?n) WHERE { VALUES ?g { " + strings.Join(values, " ") + " } GRAPH ?g { ?s ?p ?o } }"
		if err := os.WriteFile(filepath.Join(dir, "count.rq"), []byte(count), 0o644); err != nil {
			return "", nil, refuse("io", "", "%v", err)
		}
		out, err := container.RunTool(lctx, cfg, image, dir, "query", "--location", "/q/store", "--query-file", "/q/count.rq", "--results-format", "json")
		if err != nil {
			return "", nil, refuse("query.failed", "ADR-020", "counting the loaded graphs: %v", err)
		}
		res, err := oxigraph.ParseResults([]byte(out))
		if err != nil || len(res.Terms) != 1 {
			return "", nil, refuse("query.failed", "ADR-020", "counting the loaded graphs: %v", err)
		}
		n, _ := strconv.ParseInt(res.Terms[0]["n"].Value, 10, 64)
		if limit := cfg.Raw.Index.Derived.MaxTriplesOrDefault(); n > limit {
			return "", nil, refuse("derived.refused", "ADR-020", "the loaded graphs hold %d triples, more than index.derived.maxTriples (%d)", n, limit)
		}
	}
	qctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		qctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	res, err := container.RunTool(qctx, cfg, image, dir, "query", "--location", "/q/store",
		"--query-file", "/q/query.rq", "--results-format", format)
	if qctx.Err() == context.DeadlineExceeded {
		return "", nil, refuse("query.timeout", "", "the query ran longer than %s", timeout)
	}
	if err != nil {
		return "", nil, refuse("query.failed", "", "%v", err)
	}
	return res, graphs, nil
}

// projectRegistries writes the dataset a query reads (oxigraph.Dataset): default.nt, the default
// graph, and named.nq, the claims a trusted key signed; or for the query projection claims.nt, the
// claims as nodes (oxigraph.ClaimDataset). The daemon selects and builds the same.
func projectRegistries(rctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, dataset, dir string) ([]oxigraph.ClaimGraph, error) {
	return projectUnion(rctx, cfg, keys, nil, nil, dataset, dir)
}

// projectUnion is projectRegistries over u, or over cfg's union as it is when u is nil.
func projectUnion(rctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, u *binaries.Union, pin *evalPin, dataset, dir string) ([]oxigraph.ClaimGraph, error) {
	if u == nil {
		var err error
		if u, err = binaries.OpenUnion(rctx, cfg); err != nil {
			return nil, err
		}
	}
	def, named, err := oxigraph.Dataset(u, keys)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "default.nt"), def, 0o644); err != nil {
		return nil, err
	}
	if dataset == oxigraph.DatasetClaims {
		var vc oxigraph.ViewConfig
		var sources map[string][]string
		if pin != nil {
			vc, sources = pin.vc, pin.sources
		} else {
			var err error
			if vc, err = oxigraph.ViewConfigFor(cfg); err != nil {
				return nil, err
			}
			inputs, err := binaries.Inputs(rctx, cfg)
			if err != nil {
				return nil, err
			}
			if sources, err = binaries.ClaimSources(inputs); err != nil {
				return nil, err
			}
		}
		claims, scopes, graphs, err := oxigraph.ClaimDataset(u, keys, vc, sources)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "scopes.nq"), scopes, 0o644); err != nil {
			return nil, err
		}
		return graphs, os.WriteFile(filepath.Join(dir, "claims.nt"), claims, 0o644)
	}
	return nil, os.WriteFile(filepath.Join(dir, "named.nq"), named, 0o644)
}

func trustedKeys(cfg *config.Config) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for path := range cfg.TierPubkeys() {
		k, err := loadPublic(path)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func loadPublic(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s is not a public key", path)
	}
	return ed25519.PublicKey(raw), nil
}
