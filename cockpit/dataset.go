package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	graphs, err := projectRegistries(ctx, cfg, keys, dataset, dir)
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
	if _, err := container.RunTool(ctx, cfg, cfg.Raw.Query.Image, dir, append([]string{"load", "--location", "/q/store"}, files...)...); err != nil {
		return "", nil, refuse("query.failed", "", "%v", err)
	}
	qctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		qctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	res, err := container.RunTool(qctx, cfg, cfg.Raw.Query.Image, dir, "query", "--location", "/q/store",
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
	u, err := binaries.OpenUnion(rctx, cfg)
	if err != nil {
		return nil, err
	}
	def, named, err := oxigraph.Dataset(u, keys)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "default.nt"), def, 0o644); err != nil {
		return nil, err
	}
	if dataset == oxigraph.DatasetClaims {
		vc, err := oxigraph.ViewConfigFor(cfg)
		if err != nil {
			return nil, err
		}
		inputs, err := binaries.Inputs(rctx, cfg)
		if err != nil {
			return nil, err
		}
		sources, err := binaries.ClaimSources(inputs)
		if err != nil {
			return nil, err
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
