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
		Slot    string    `json:"slot"`
		Key     string    `json:"key"`
		Graphs  *[]string `json:"graphs"`
		Removed *[]string `json:"removed"`
		All     bool      `json:"all"`
	}
	must(t, json.Unmarshal(got[len(got)-1].Params, &moved))
	if moved.Slot == "" || moved.Key == "" || moved.Graphs == nil {
		t.Errorf("indexMoved names the view, its key and its graphs: %s", got[len(got)-1].Params)
	}
	if !moved.All {
		t.Errorf("no view when the connection started: the first indexMoved says all: %s", got[len(got)-1].Params)
	}
	first := moved
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
	// A second claim, no scope: the event names the graphs it rewrote, the unscoped claims' among them,
	// and no scope's graph (SPEC §12.5).
	c.ok("say", map[string]any{"subject": "urn:example:y", "template": "describes",
		"fields": map[string]string{"value": "indexed again"}})
	got = c.waitEvent("events/indexMoved")
	moved.Graphs, moved.Removed, moved.All = nil, nil, false
	must(t, json.Unmarshal(got[len(got)-1].Params, &moved))
	p := string(got[len(got)-1].Params)
	if moved.All || moved.Graphs == nil || moved.Removed == nil || moved.Key == first.Key || moved.Slot != first.Slot {
		t.Fatalf("second indexMoved: want this view's new key and its graphs, not all: %s", p)
	}
	unscoped := false
	for _, g := range *moved.Graphs {
		unscoped = unscoped || g == "urn:kton:view:"+moved.Slot+":unscoped"
		if strings.Contains(g, ":scope:") {
			t.Errorf("a claim with no scope rewrote a scope's graph %s: %s", g, p)
		}
	}
	if !unscoped {
		t.Errorf("the unscoped claims' graph changed and is named: %s", p)
	}

	// A new connection without a cursor of its own, on the view that exists now: its first indexMoved
	// names what changed after it started, not all (kton-review F1 on #259).
	c2 := startServe(t)
	c2.ok("initialize", map[string]any{"root": r.Root, "events": map[string]any{"intervalMs": 500}})
	c2.ok("say", map[string]any{"subject": "urn:example:z", "template": "describes",
		"fields": map[string]string{"value": "indexed a third time"}})
	got = c2.waitEvent("events/indexMoved")
	moved.Graphs, moved.Removed, moved.All = nil, nil, false
	must(t, json.Unmarshal(got[len(got)-1].Params, &moved))
	if p := string(got[len(got)-1].Params); moved.All || moved.Graphs == nil || len(*moved.Graphs) == 0 {
		t.Errorf("a new connection on an existing view: want the graphs that changed, not all: %s", p)
	}
	_ = res
}
