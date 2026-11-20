//go:build docker

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// With the index, events/indexMoved names the view and comes once the index holds the claim: a query
// sent right after it finds the claim without building anything (SPEC §12.5).
func TestServe_IndexMovedComesOnceTheIndexHoldsTheClaim(t *testing.T) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = "oxigraph"
	r.WriteConfig(t, raw)
	r.Use(t)
	t.Cleanup(func() { oxigraph.Stop(context.Background(), r.Config(t)) })
	c := startServe(t)
	c.ok("initialize", map[string]any{"root": r.Root, "events": map[string]any{"intervalMs": 500}})
	var res struct {
		ClaimID string `json:"claimId"`
	}
	must(t, json.Unmarshal(c.ok("say", map[string]any{"subject": "urn:example:x", "template": "describes",
		"fields": map[string]string{"value": "indexed"}}), &res))
	got := c.waitEvent("events/indexMoved")
	var moved struct {
		Slot string `json:"slot"`
	}
	must(t, json.Unmarshal(got[len(got)-1].Params, &moved))
	if moved.Slot == "" {
		t.Errorf("indexMoved names the view: %s", got[len(got)-1].Params)
	}
	var a struct {
		Sparql struct {
			Rows []map[string]string `json:"rows"`
			Via  string              `json:"via"`
		} `json:"sparql"`
	}
	must(t, json.Unmarshal(c.ok("ask", map[string]any{"query": "sparql",
		"sparql": `SELECT ?c WHERE { ?c <https://kton.dev/v/lab/value> "indexed" }`}), &a))
	if len(a.Sparql.Rows) != 1 || !strings.Contains(a.Sparql.Via, "(current)") {
		t.Errorf("right after indexMoved, the index holds the claim and builds nothing: %+v", a.Sparql)
	}
	_ = res
}
