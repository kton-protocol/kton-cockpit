//go:build docker

package cli

import (
	"testing"
)

const oxigraphImage = "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"

// A query tool runs the package's query with the server's binding and the caller's arguments, as
// ask sparql --name would: with $LANE fixed, the lane sees its own area only (SPEC §6.2).
func TestMCP_AQueryToolRunsWithTheServersBinding(t *testing.T) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	vocabRepo(t, oxigraphImage)
	cs := connect(t, mcpOptions{vocabulary: true, as: "lane-a", scope: "lane-a", bind: map[string]string{"LANE": "kton-cockpit"}})
	for _, area := range []string{"kton-cockpit", "kton"} {
		if _, why := callTool(t, cs, "say_requests", map[string]any{"area": area, "title": "for " + area}); why != "" {
			t.Fatalf("say_requests: %s", why)
		}
	}
	out, why := callTool(t, cs, "ask_open", map[string]any{})
	if why != "" {
		t.Fatalf("ask_open: %s", why)
	}
	rows, _ := out["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "for kton-cockpit" {
		t.Fatalf("want the one request in the bound lane's area: %v", out)
	}
	out, why = callTool(t, cs, "ask_all", map[string]any{"limit": 1})
	if rows, _ := out["rows"].([]any); why != "" || len(rows) != 1 {
		t.Fatalf("ask_all with limit 1: want one row: %v %s", out, why)
	}
}
