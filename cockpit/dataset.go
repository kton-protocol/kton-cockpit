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
func inContainer(ctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, q []byte, format string, timeout time.Duration) (string, error) {
	dir, err := os.MkdirTemp("", "cockpit-query-")
	if err != nil {
		return "", refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(dir)
	if err := projectRegistries(ctx, cfg, keys, dir); err != nil {
		return "", refuse("kernel", "", "projecting the registries: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "query.rq"), q, 0o644); err != nil {
		return "", refuse("io", "", "%v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "store"), 0o755); err != nil {
		return "", refuse("io", "", "%v", err)
	}
	if _, err := container.RunTool(ctx, cfg, cfg.Raw.Query.Image, dir, "load", "--location", "/q/store",
		"--file", "/q/default.nt", "--file", "/q/named.nq"); err != nil {
		return "", refuse("query.failed", "", "%v", err)
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
		return "", refuse("query.timeout", "", "the query ran longer than %s", timeout)
	}
	if err != nil {
		return "", refuse("query.failed", "", "%v", err)
	}
	return res, nil
}

// projectRegistries writes the dataset a query reads (oxigraph.Dataset): default.nt, the default
// graph, and named.nq, the claims a trusted key signed. The daemon selects the same graphs.
func projectRegistries(rctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, dir string) error {
	u, err := binaries.OpenUnion(rctx, cfg)
	if err != nil {
		return err
	}
	def, named, err := oxigraph.Dataset(u, keys)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "default.nt"), def, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "named.nq"), named, 0o644)
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
