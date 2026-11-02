package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// vocabRepo is a repository with a lane key in its keyring and trust tiers, the lane's scope, and
// an allowed package bringing a template and two queries, one of them taking $LANE. It returns the
// scope's id.
func vocabRepo(t *testing.T, queryImage ...string) string {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	r := testrepo.New(t)
	seed := r.SeedScope(t, "lane-a")
	ring := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	must(os.WriteFile(filepath.Join(ring, "lane-a-claims.key"), []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600))
	must(os.WriteFile(filepath.Join(ring, "lane-a-claims.pub"), []byte(hex.EncodeToString(pub)+"\n"), 0o644))
	r.Write(t, "registry/keys/lane-a-claims.pub", hex.EncodeToString(pub)+"\n")
	raw := testrepo.DefaultConfig()
	raw.Identity.Keyring = ring
	raw.Trust.Tiers["lanes"] = []string{"registry/keys/lane-a-claims.pub"}
	raw.Claims.Scopes = map[string]string{"lane-a": seed}
	if len(queryImage) > 0 {
		raw.Query.Image = queryImage[0]
	}
	r.WriteConfig(t, raw)
	r.Use(t)

	src := t.TempDir()
	write := func(p, s string) {
		must(os.MkdirAll(filepath.Dir(p), 0o755))
		must(os.WriteFile(p, []byte(s), 0o644))
	}
	write(filepath.Join(src, "templates", "requests.json"), `{"name": "requests", "target": "either",
  "predicate": "https://example.org/ln/requests", "fields": {
    "area": {"type": "string", "required": true}, "title": {"type": "string", "required": true},
    "fullText": {"type": "string", "required": false}}}`)
	write(filepath.Join(src, "queries", "open.rq"), `PREFIX nk: <https://kton.dev/v/>
# Open for $LANE: the requests in its area.
#
# More about it, not in the description.
SELECT ?msg ?title WHERE { ?msg nk:predicate <https://example.org/ln/requests> ;
  <https://kton.dev/v/lab/area> $LANE ; <https://kton.dev/v/lab/title> ?title }`)
	write(filepath.Join(src, "queries", "all.rq"), `PREFIX nk: <https://kton.dev/v/>
# Every request.
SELECT ?msg WHERE { ?msg nk:predicate <https://example.org/ln/requests> }`)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	pkg := filepath.Join(t.TempDir(), "inbox")
	_, _, err = scope.BuildTemplates(pkg, "inbox", "v1", filepath.Join(src, "templates"), filepath.Join(src, "queries"), key, scope.Options{})
	must(err)
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: pkg, Allow: true}); err != nil {
		t.Fatal(err)
	}
	return seed
}

func connect(t *testing.T, o mcpOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server, err := mcpServer(ctx, o)
	if err != nil {
		t.Fatalf("mcp server: %v", err)
	}
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err.Error()
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return nil, b.String()
	}
	b, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out, ""
}

// With --vocabulary, the allowed packages' queries and templates are tools of their own: each one
// of the verbs with arguments fixed. What the operator fixed (identity, scope, a bound parameter)
// is not in the schema, so a session cannot choose it; and --as holds for cockpit_say too
// (SPEC §6.2).
func TestMCP_VocabularyToolsAreTheVerbsWithArgumentsFixed(t *testing.T) {
	seed := vocabRepo(t)
	cs := connect(t, mcpOptions{vocabulary: true, as: "lane-a", scope: "lane-a", bind: map[string]string{"LANE": "kton-cockpit"}})

	tools := map[string]*mcp.Tool{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = tool
	}
	for _, n := range []string{"cockpit_publish", "cockpit_say", "cockpit_ask", "ask_open", "ask_all", "say_requests"} {
		if tools[n] == nil {
			t.Fatalf("no tool %s among %v", n, tools)
		}
	}
	schema := func(n string) (props map[string]any, required []string) {
		b, _ := json.Marshal(tools[n].InputSchema)
		var s struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		json.Unmarshal(b, &s)
		return s.Properties, s.Required
	}
	if props, _ := schema("ask_open"); props["LANE"] != nil {
		t.Errorf("the bound $LANE must not be a parameter: %v", props)
	}
	if d := tools["ask_open"].Description; !strings.Contains(d, "Open for $LANE") || strings.Contains(d, "More about it") ||
		!strings.Contains(d, "LANE=kton-cockpit") {
		t.Errorf("want the query's first comment paragraph and the fixed binding: %q", d)
	}
	props, required := schema("say_requests")
	if props["subject"] == nil || slices.Contains(required, "subject") || !slices.Equal(required, []string{"area", "title"}) {
		t.Errorf("say_requests: want area and title required, subject optional with a scope: %v %v", props, required)
	}

	out, why := callTool(t, cs, "say_requests", map[string]any{"area": "kton-cockpit", "title": "a request"})
	if why != "" {
		t.Fatalf("say_requests: %s", why)
	}
	id, _ := out["claimId"].(string)
	chain, _ := out["chain"].(map[string]any)
	if id == "" || chain == nil || chain["scope"] != seed {
		t.Fatalf("want a claim chained into the lane's scope %s: %v", seed, out)
	}
	feed, err := cockpit.New(cockpit.Start{}).Ask(context.Background(), cockpit.AskRequest{Query: "feed"})
	if err != nil {
		t.Fatal(err)
	}
	var signers []cockpit.FeedSigner
	for _, rec := range feed.Feed.Records {
		if rec.ID == id {
			signers = rec.Signers
		}
	}
	if len(signers) != 1 || signers[0].Tier != "lanes" {
		t.Errorf("want the claim signed as lane-a, in tier lanes: %+v", signers)
	}

	for name, args := range map[string]map[string]any{
		"say_requests": {"area": "x", "title": "t", "lane": "typo"},
		"cockpit_say":  {"subject": seed, "template": "requests", "as": "someone-else", "fields": map[string]any{"area": "x", "title": "t"}},
	} {
		if _, why := callTool(t, cs, name, args); why == "" {
			t.Errorf("%s %v: want refused", name, args)
		}
	}
	if _, why := callTool(t, cs, "cockpit_say", map[string]any{"subject": seed, "template": "requests",
		"fields": map[string]any{"area": "x", "title": "t"}}); why != "" {
		t.Errorf("cockpit_say without as signs as the server's identity: %s", why)
	}
}

// Without --vocabulary the surface is the three verbs; the options that only fix vocabulary tools
// are refused without it, and an unknown scope is refused at start rather than on the first call.
func TestMCP_TheVocabularyIsOptInAndItsOptionsAreChecked(t *testing.T) {
	vocabRepo(t)
	cs := connect(t, mcpOptions{})
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"cockpit_ask", "cockpit_publish", "cockpit_say"}) {
		t.Errorf("want the three verbs only: %v", names)
	}
	for _, args := range [][]string{{"--scope", "lane-a"}, {"--bind", "LANE=x"}, {"--vocabulary", "--bind", "a b=x"}, {"--as"}, {"--nope"}} {
		if _, err := parseMCPArgs(args); err == nil {
			t.Errorf("%v: want refused", args)
		}
	}
	if _, err := mcpServer(context.Background(), mcpOptions{vocabulary: true, scope: "nowhere"}); err == nil {
		t.Error("an unconfigured scope: want refused at start")
	}
}
