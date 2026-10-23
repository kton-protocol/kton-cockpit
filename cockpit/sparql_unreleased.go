//go:build unreleased

package cockpit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// runSparql evaluates a free query where a vocabulary query is evaluated (query.go): on the index
// daemon when index.kind is oxigraph, otherwise in a container for this query alone. Both read the
// same dataset, so the rows do not depend on where it ran.
func runSparql(ctx context.Context, cfg *config.Config, q string, timeout time.Duration) (*SparqlAnswer, error) {
	if cfg.Raw.Query.Image == "" && cfg.Raw.Index.URL == "" {
		return nil, refuse("query.not-configured", "SPEC §9.9", "no SPARQL engine configured (query.image in cockpit.config.json)")
	}
	form, err := index.CheckFreeQuery(q)
	if err != nil {
		return nil, refuse("query.refused", "SPEC §9.9", "%v", err)
	}
	keys, err := trustedKeys(cfg)
	if err != nil {
		return nil, refuse("trust", "", "%v", err)
	}
	via := "a container for this query"
	if cfg.Raw.Index.Kind == "oxigraph" {
		d, err := oxigraph.Ensure(ctx, cfg)
		if err == nil {
			a, err := oxigraph.Free(ctx, d, cfg, keys, q, timeout)
			if err != nil {
				code := "query.failed"
				if errors.Is(err, oxigraph.ErrTimeout) {
					code = "query.timeout"
				}
				return nil, refuse(code, "SPEC §9.9", "%v", err)
			}
			kept := "read into it for this query"
			if a.Kept {
				kept = "current"
			}
			return answerOf(form, &a.Results, fmt.Sprintf("index oxigraph, %d registries (%s)", a.Registries, kept)), nil
		}
		if cfg.Raw.Query.Image == "" {
			return nil, refuse("query.failed", "", "%v", err)
		}
		via += "; not the index: " + err.Error()
	}
	body, err := inContainer(ctx, cfg, keys, []byte(q), "json", timeout)
	if err != nil {
		return nil, err
	}
	res, err := oxigraph.ParseResults([]byte(body))
	if err != nil {
		return nil, refuse("query.failed", "", "%v", err)
	}
	return answerOf(form, res, via), nil
}

func answerOf(form string, r *oxigraph.Results, via string) *SparqlAnswer {
	return &SparqlAnswer{Form: form, Vars: r.Vars, Rows: r.Rows, Boolean: r.Boolean, Via: via}
}
