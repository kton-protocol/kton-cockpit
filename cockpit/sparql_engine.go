package cockpit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// runSparql evaluates a free query where a vocabulary query is evaluated (query.go): on the index
// daemon when index.kind is oxigraph, otherwise in a container for this query alone. Both read the
// same dataset, so the rows do not depend on where it ran.
func runSparql(ctx context.Context, cfg *config.Config, q, dataset string, timeout time.Duration) (*SparqlAnswer, error) {
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
			missing := 0
			if cfg.Raw.Index.Derived.Enabled {
				if missing, err = reloadMissing(ctx, cfg, d); err != nil {
					return nil, refuse("index", "SPEC §12.6", "loading again what this checkout loaded: %v", err)
				}
			}
			a, err := oxigraph.Free(ctx, d, cfg, keys, q, dataset, timeout)
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
			ans := answerOf(form, &a.Results, fmt.Sprintf("index oxigraph, %d registries (%s)", a.Registries, kept))
			ans.LoadedGraphs, ans.LoadMissing = a.Loaded, missing
			ans.setGraphs(a.Graphs)
			return ans, nil
		}
		if cfg.Raw.Query.Image == "" {
			return nil, refuse("query.failed", "", "%v", err)
		}
		via += "; not the index: " + err.Error()
	}
	body, graphs, err := inContainer(ctx, cfg, keys, []byte(q), "json", dataset, timeout)
	if err != nil {
		// Both ways failed: say why the index was not used too, or only the second failure is seen
		// (userinterface, 21.11.: a container error hid that the index could not be started).
		var ref *Refusal
		if strings.Contains(via, "not the index") && errors.As(err, &ref) {
			ref.Reason += " (" + strings.TrimPrefix(via, "a container for this query; ") + ")"
		}
		return nil, err
	}
	res, err := oxigraph.ParseResults([]byte(body))
	if err != nil {
		return nil, refuse("query.failed", "", "%v", err)
	}
	ans := answerOf(form, res, via)
	ans.setGraphs(graphs)
	return ans, nil
}

func answerOf(form string, r *oxigraph.Results, via string) *SparqlAnswer {
	return &SparqlAnswer{Form: form, Vars: r.Vars, Rows: r.Rows, Boolean: r.Boolean, Via: via, terms: r.Terms}
}
