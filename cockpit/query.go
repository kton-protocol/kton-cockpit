//go:build unreleased

package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	nclaim "kton.dev/nekton/claim"
	"kton.dev/nekton/nanopub"
	nregistry "kton.dev/nekton/registry"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/core"
	"kton.dev/plankton/rdf"
	pregistry "kton.dev/plankton/registry"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/container"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// Search and profile rules are SPARQL over the registries' RDF projection (kton K8), evaluated in a
// pinned Oxigraph image. Only what verifies against this repository's trust tiers is projected: a
// query answers over the same records ask would include, never over what merely arrived.

// runQuery evaluates the named query from the allowed vocabulary and returns its rows. The second
// value says which package the query came from.
func runQuery(ctx context.Context, cfg *config.Config, name string) ([]map[string]string, string, error) {
	if cfg.Raw.Query.Image == "" {
		return nil, "", refuse("query.not-configured", "", "no SPARQL engine configured (query.image in cockpit.config.json)")
	}
	voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return nil, "", refuse("vocabulary", "", "%v", err)
	}
	q, ok := voc.Queries[name]
	if !ok {
		return nil, "", refuse("query.unknown", "", "no allowed package brings the query %q — install a vocabulary package and name it in claims.allowedPackages", name)
	}
	src := voc.From["query:"+name]

	keys, err := trustedKeys(cfg)
	if err != nil {
		return nil, "", refuse("trust", "", "%v", err)
	}
	dir, err := os.MkdirTemp("", "cockpit-query-")
	if err != nil {
		return nil, "", refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(dir)
	if err := projectRegistries(ctx, cfg, keys, dir); err != nil {
		return nil, "", refuse("kernel", "", "projecting the registries: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "query.rq"), q, 0o644); err != nil {
		return nil, "", refuse("io", "", "%v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "store"), 0o755); err != nil {
		return nil, "", refuse("io", "", "%v", err)
	}
	if _, err := container.RunTool(ctx, cfg, cfg.Raw.Query.Image, dir, "load", "--location", "/q/store",
		"--file", "/q/nekton.trig", "--file", "/q/plankton.ttl"); err != nil {
		return nil, "", refuse("query.failed", "", "%v", err)
	}
	res, err := container.RunTool(ctx, cfg, cfg.Raw.Query.Image, dir, "query", "--location", "/q/store",
		"--query-file", "/q/query.rq", "--results-format", "csv")
	if err != nil {
		return nil, "", refuse("query.failed", "", "%v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(res)).ReadAll()
	if err != nil {
		return nil, "", refuse("query.failed", "", "reading the result: %v", err)
	}
	var out []map[string]string
	for i, row := range rows {
		if i == 0 {
			continue
		}
		m := map[string]string{}
		for j, v := range row {
			if j < len(rows[0]) {
				m[rows[0][j]] = strings.Replace(v, "https://kton.dev/o/", "sha256:", 1)
			}
		}
		out = append(out, m)
	}
	return out, src.Name + " (" + src.Dir + ")", nil
}

// projectRegistries writes nekton.trig and plankton.ttl for the records a trusted key signed.
func projectRegistries(rctx context.Context, cfg *config.Config, keys []ed25519.PublicKey, dir string) error {
	pdir, ndir, err := binaries.ReadDirs(rctx, cfg)
	if err != nil {
		return err
	}
	nreg, err := nregistry.Open(ndir)
	if err != nil {
		return err
	}
	ctx := nanopub.NewContext(ntemplate.Set{}, keys)
	var b strings.Builder
	for _, rec := range nreg.Records(0) {
		if core.VerifiedSignerKeyID(rec.Envelope, keys) == "" {
			continue
		}
		st, payload, err := nclaim.ParseEnvelope(rec.Envelope)
		if err != nil {
			continue
		}
		var body map[string]any
		if json.Unmarshal(st.Predicate, &body) != nil {
			continue
		}
		id := strings.TrimPrefix(nclaim.ClaimID(payload), "sha256:")
		b.WriteString(nanopub.RenderTrig(ctx, st, body, rec.Envelope, id, "", "", ""))
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "nekton.trig"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	preg, err := pregistry.Open(pdir)
	if err != nil {
		return err
	}
	ttl, err := rdf.Turtle(preg, "", keys)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "plankton.ttl"), []byte(ttl), 0o644)
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
