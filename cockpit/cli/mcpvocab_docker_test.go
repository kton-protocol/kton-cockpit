//go:build docker

package cli

import (
	"os"
	"path/filepath"
	"strings"
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

const alpineImage = "oci://alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"

// Calling a declared ray tool binds the input to the holes, takes the package's default for an
// optional hole left out, and records the step in the image the step names, which is the one this
// repository runs steps in; the run lands in a directory named after its source (SPEC §6.2).
func TestMCP_ADeclaredRayToolRunsItsSteps(t *testing.T) {
	r := verticleRepo(t, alpineImage, alpineImage)
	cs := connect(t, mcpOptions{runs: true})
	out, why := callTool(t, cs, "greet", map[string]any{"who": "data/ada.txt"})
	if why != "" {
		t.Fatalf("greet: %s", why)
	}
	if out["dir"] != "work/greet/ada" {
		t.Errorf("want the run in work/greet/ada: %v", out["dir"])
	}
	steps, _ := out["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["fotonId"] == "" {
		t.Fatalf("want one recorded step: %v", out)
	}
	b, err := os.ReadFile(filepath.Join(r.Root, "work", "greet", "ada", "greet", "greeting.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "hello Ada!" {
		t.Errorf("want the source bound and the package's suffix: %q %v (the run: %v)", b, err, out)
	}
}
