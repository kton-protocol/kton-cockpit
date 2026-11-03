package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// verticleRepo installs and allows a verticle declaring two MCP tools: "greet", bound to a ray of
// one step (the source hole required, the suffix hole optional with the package's own default),
// and "open", bound to a query. image, when given, is the step's Protocol.Image; execImage, when
// given, the repository's execution.image (a step runs only where the cockpit executes it).
func verticleRepo(t *testing.T, image, execImage string) *testrepo.Repo {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	r := testrepo.New(t)
	if execImage != "" {
		raw := testrepo.DefaultConfig()
		raw.Execution = config.Execution{Image: execImage}
		r.WriteConfig(t, raw)
	}
	r.Use(t)
	src := t.TempDir()
	write := func(p, s string) {
		must(os.MkdirAll(filepath.Dir(filepath.Join(src, p)), 0o755))
		must(os.WriteFile(filepath.Join(src, p), []byte(s), 0o644))
	}
	proto := map[string]any{"command": map[string]string{"args": "sh\n<command-file>"}}
	if image != "" {
		proto["image"] = image
	}
	ray, _ := json.Marshal(map[string]any{"name": "greet", "steps": []any{map[string]any{
		"id": "greet", "protocol": proto,
		"inputs": []any{
			map[string]string{"slot": "greet.sh", "path": "steps/greet.sh", "variable": "command-file"},
			map[string]string{"slot": "name.txt", "from": "hole:name"},
			map[string]string{"slot": "suffix.txt", "from": "hole:suffix"},
		},
		"outputs": []string{"greeting.txt"},
	}}, "holes": []any{
		map[string]any{"name": "name", "step": "greet", "slot": "name.txt", "type": "file", "required": true, "default": "testdata/name.txt"},
		map[string]any{"name": "suffix", "step": "greet", "slot": "suffix.txt", "type": "file", "required": false, "default": "testdata/suffix.txt"},
	}})
	write("ray.json", string(ray))
	write("steps/greet.sh", "printf 'hello %s%s\\n' \"$(cat name.txt)\" \"$(cat suffix.txt)\" > greeting.txt\n")
	write("testdata/name.txt", "test")
	write("testdata/suffix.txt", "!")
	write("queries/open.rq", "PREFIX nk: <https://kton.dev/v/>\n# Requests in an area.\nSELECT ?msg WHERE { ?msg nk:predicate ?p ; <https://kton.dev/v/lab/area> $AREA }\n")
	write("mcp/greet.json", `{"description": "Greet someone.", "ray": "greet", "bind": {"name": "who", "suffix": "ending"},
  "input": {"type": "object", "required": ["who"], "properties": {"who": {"type": "string"}, "ending": {"type": "string"}}}}`)
	write("mcp/open.json", `{"description": "Open requests.", "query": "open", "bind": {"AREA": "area"},
  "input": {"type": "object", "required": ["area"], "properties": {"area": {"type": "string"}}}}`)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	pkg := filepath.Join(t.TempDir(), "greet")
	if _, _, err := scope.BuildVerticle(pkg, "greet", "1", src, key, scope.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true}); err != nil {
		t.Fatal(err)
	}
	r.Write(t, "data/ada.txt", "Ada")
	return r
}

func listTools(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	out := map[string]*mcp.Tool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		out[tool.Name] = tool
	}
	return out
}

// A verticle's declared tools are served under their own names and schemas: a query tool with
// --vocabulary, a ray tool only with --runs, and then instead of the derived run_<name>; a required
// input left out is refused before anything runs (SPEC §6.2).
func TestMCP_AVerticlesDeclaredToolsAreServed(t *testing.T) {
	r := verticleRepo(t, "", "")

	tools := listTools(t, connect(t, mcpOptions{vocabulary: true}))
	if tools["open"] == nil || tools["greet"] != nil {
		t.Fatalf("--vocabulary: want the query tool open and no ray tool: %v", tools)
	}
	cs := connect(t, mcpOptions{runs: true})
	tools = listTools(t, cs)
	if tools["greet"] == nil || tools["run_greet"] != nil || tools["open"] != nil {
		t.Fatalf("--runs: want the declared greet, not run_greet, and no query tool: %v", tools)
	}
	if !strings.Contains(tools["greet"].Description, "Greet someone.") {
		t.Errorf("want the declared description: %q", tools["greet"].Description)
	}
	if _, why := callTool(t, cs, "greet", map[string]any{}); why == "" {
		t.Error("the required input left out: want refused")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "work", "greet")); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(r.Root, "work", "greet")); len(entries) > 0 {
			t.Errorf("a refused call left files in work/: %v", entries)
		}
	}
}

// A step that names an image other than the one this repository runs steps in is refused before
// anything runs, rather than run somewhere else (SPEC §7.5).
func TestWorkflowRun_AStepInAnotherImageIsRefused(t *testing.T) {
	r := verticleRepo(t, "oci://example.org/other@sha256:"+strings.Repeat("a", 64), "oci://example.org/mine@sha256:"+strings.Repeat("b", 64))
	_, err := cockpit.New(cockpit.Start{}).WorkflowRun(context.Background(),
		cockpit.WorkflowRunRequest{Name: "greet", Bindings: map[string]string{"name": "data/ada.txt"}})
	if err == nil || !strings.Contains(err.Error(), "execution.image") {
		t.Fatalf("want refused, naming execution.image: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(r.Root, "work", "greet")); statErr == nil {
		t.Error("nothing should have been staged")
	}
}
