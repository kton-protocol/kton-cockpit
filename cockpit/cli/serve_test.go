package cli

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/frontrpc"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// serveClient talks to serve() over pipes, as an editor extension talks to `cockpit serve --stdio`.
type serveClient struct {
	t  *testing.T
	w  io.Writer
	r  *bufio.Reader
	id atomic.Int64
}

type serveReply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func startServe(t *testing.T) *serveClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		_ = serve(context.Background(), inR, outW)
		outW.Close()
		close(done)
	}()
	t.Cleanup(func() { inW.Close(); <-done })
	return &serveClient{t: t, w: inW, r: bufio.NewReader(outR)}
}

// startServeBinary runs `cockpit serve --stdio` itself, so an answer that depends on how the binary was
// built (version, doctor's kernels) is compared with the same binary's command line.
func startServeBinary(t *testing.T, r *testrepo.Repo) *serveClient {
	t.Helper()
	cmd := r.CockpitCommand(t, "serve", "--stdio")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); _ = cmd.Wait() })
	return &serveClient{t: t, w: in, r: bufio.NewReader(out)}
}

func (c *serveClient) call(method string, params any) serveReply {
	c.t.Helper()
	id := c.id.Add(1)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err := frontrpc.WriteMessage(c.w, b); err != nil {
		c.t.Fatal(err)
	}
	body, err := frontrpc.ReadMessage(c.r)
	if err != nil {
		c.t.Fatal(err)
	}
	var r serveReply
	if err := json.Unmarshal(body, &r); err != nil {
		c.t.Fatalf("%s: %v", body, err)
	}
	return r
}

func (c *serveClient) ok(method string, params any) json.RawMessage {
	c.t.Helper()
	r := c.call(method, params)
	if r.Error != nil {
		c.t.Fatalf("%s: %d %s %s", method, r.Error.Code, r.Error.Message, r.Error.Data)
	}
	return r.Result
}

// refusalCode is the CLI refusal code a serve error carries in its data.
func (r serveReply) refusalCode() string {
	if r.Error == nil {
		return ""
	}
	var d struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Error.Data, &d)
	return d.Code
}

func sameJSON(t *testing.T, what string, a, b []byte) {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("%s: %v\n%s", what, err, a)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("%s: %v\n%s", what, err, b)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("%s differs from the command line's --json:\nserve: %s\ncli:   %s", what, a, b)
	}
}

// A connection serves one workspace, named by initialize: nothing else answers before it, a folder the
// configuration does not bind is refused, and a second initialize is too (SPEC §12.5).
func TestServe_InitializeNamesTheWorkspaceFirst(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	c := startServe(t)
	if rep := c.call("version", nil); rep.Error == nil || rep.Error.Code != serveCodeNotInitialized {
		t.Errorf("before initialize: %+v", rep)
	}
	if rep := c.call("initialize", map[string]any{"root": t.TempDir()}); rep.refusalCode() != "binding" {
		t.Errorf("a root COCKPIT_REPO_DIR does not name, or no repository: want binding, got %+v", rep)
	}
	var init ServeInitResult
	if err := json.Unmarshal(c.ok("initialize", map[string]any{"root": r.Root}), &init); err != nil {
		t.Fatal(err)
	}
	if init.Protocol != ServeProtocol || init.Root == "" || init.Cockpit.Cockpit == "" {
		t.Errorf("initialize: %+v", init)
	}
	for _, m := range []string{"publish", "say", "ask", "say/templates", "ask/questions", "ui/list", "ui/cat", "show/snapshot", "doctor", "version"} {
		if !contains(init.Methods, m) {
			t.Errorf("initialize does not list %s: %v", m, init.Methods)
		}
	}
	if rep := c.call("initialize", map[string]any{"root": r.Root}); rep.Error == nil {
		t.Error("a second initialize on one connection: want an error")
	}
	if rep := c.call("nothing", nil); rep.Error == nil || rep.Error.Code != frontrpc.CodeMethodNotFound {
		t.Errorf("unknown method: %+v", rep)
	}
	if rep := c.call("ask", map[string]any{"query": "about", "nonsense": 1}); rep.Error == nil || rep.Error.Code != frontrpc.CodeInvalidParams {
		t.Errorf("an unknown field: want invalid params, got %+v", rep)
	}
}

// Every listing over serve is the command line's --json answer, by the same function (SPEC §12.5).
func TestServe_ListingsAreTheCommandLinesJSON(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "words"), "words", "v1", key, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	r.Cockpit(t, "--version") // builds the binary
	c := startServeBinary(t, r)
	c.ok("initialize", map[string]any{"root": r.Root})

	cli := func(args ...string) []byte {
		t.Helper()
		stdout, _, _ := r.CockpitSplit(t, args...)
		return []byte(stdout)
	}
	sameJSON(t, "say/templates", c.ok("say/templates", nil), cli("say", "--json"))
	sameJSON(t, "ask/questions", c.ok("ask/questions", nil), cli("ask", "--json"))
	sameJSON(t, "ui/list", c.ok("ui/list", nil), cli("ui", "list", "--json"))
	sameJSON(t, "doctor", c.ok("doctor", nil), cli("doctor", "--json"))
	sameJSON(t, "version", c.ok("version", nil), cli("version", "--json"))

	var l UIListing
	if err := json.Unmarshal(c.ok("ui/list", nil), &l); err != nil || len(l.Packages) != 1 || len(l.Packages[0].Members) == 0 {
		t.Fatalf("ui/list: %v %+v", err, l)
	}
	h := l.Packages[0].Members[0].Hash
	sameJSON(t, "ui/cat", c.ok("ui/cat", map[string]any{"package": "words", "hashes": []string{h}}), cli("ui", "cat", "words", h, "--json"))
	missing := "sha256:" + strings.Repeat("0", 64)
	if rep := c.call("ui/cat", map[string]any{"package": "words", "hashes": []string{h, missing}}); rep.refusalCode() != "ui.member" || rep.Result != nil {
		t.Errorf("ui/cat with a non-member: want the whole call refused ui.member, got %+v", rep)
	}
}

// The verbs over serve: a claim is said and answered as the Go API answers it, and a refusal is a
// JSON-RPC error carrying the CLI's {code, clause, reason}. A connection that signs as one identity
// refuses a say naming another (SPEC §12.5, SPEC §8.7).
func TestServe_VerbsAnswerAndRefuseAsTheCommandLine(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	c := startServe(t)
	c.ok("initialize", map[string]any{"root": r.Root})

	var said struct {
		ClaimID string `json:"claimId"`
	}
	if err := json.Unmarshal(c.ok("say", map[string]any{"template": "working-on", "subject": "urn:example:x",
		"fields": map[string]string{"step": "analysis", "by-session": "s"}}), &said); err != nil || !strings.HasPrefix(said.ClaimID, "sha256:") {
		t.Fatalf("say: %v %+v", err, said)
	}
	by := c.ok("ask", map[string]any{"query": "by", "axis": "predicate", "ref": "https://kton.dev/v/working-on"})
	if !strings.Contains(string(by), said.ClaimID) {
		t.Errorf("ask by predicate does not find the claim said over serve:\n%s", by)
	}

	rep := c.call("say", map[string]any{"template": "not-allowed", "subject": "urn:example:x"})
	if rep.Error == nil || rep.Error.Code != serveCodeRefusal || rep.refusalCode() != "template.not-allowed" || rep.Error.Message == "" {
		t.Errorf("a template outside the ceiling: want refusal template.not-allowed, got %+v", rep)
	}

	// a connection signs as one identity of the keyring, checked when it opens
	if rep := startServe(t).call("initialize", map[string]any{"root": r.Root, "as": "nobody"}); rep.refusalCode() != "identity.as" {
		t.Errorf("initialize as an identity this checkout cannot sign as: want identity.as, got %+v", rep)
	}
	ring := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	os.WriteFile(filepath.Join(ring, "lane-a-claims.key"), []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600)
	os.WriteFile(filepath.Join(ring, "lane-a-claims.pub"), []byte(hex.EncodeToString(pub)+"\n"), 0o644)
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", hex.EncodeToString(pub)+"\n")
	raw := r.Config(t).Raw
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.SetKeyring(t, ring)
	r.WriteConfig(t, raw)

	signed := startServe(t)
	signed.ok("initialize", map[string]any{"root": r.Root, "as": "lane-a"})
	if rep := signed.call("say", map[string]any{"template": "working-on", "subject": "urn:example:x", "as": "else",
		"fields": map[string]string{"step": "analysis", "by-session": "s"}}); rep.refusalCode() != "identity.as" || !strings.Contains(rep.Error.Message, `this connection signs as "lane-a"`) {
		t.Errorf("a say naming another identity than the connection's: want identity.as from the connection, got %+v", rep)
	}
	var mine struct {
		ClaimID string `json:"claimId"`
	}
	if err := json.Unmarshal(signed.ok("say", map[string]any{"template": "working-on", "subject": "urn:example:x",
		"fields": map[string]string{"step": "analysis", "by-session": "s"}}), &mine); err != nil {
		t.Fatal(err)
	}
	byLane := c.ok("ask", map[string]any{"query": "by", "axis": "signer", "ref": keyIDOfHex(t, hex.EncodeToString(pub))})
	if !strings.Contains(string(byLane), mine.ClaimID) {
		t.Errorf("a say naming no identity over a connection signing as lane-a was not signed by lane-a:\n%s", byLane)
	}
}

// COCKPIT_REPO_DIR would replace the folder a connection serves; serve refuses rather than serve
// another repository than the window names.
func TestServe_RefusesAWorkspaceTheEnvironmentWouldRedirect(t *testing.T) {
	r := testrepo.New(t)
	other := testrepo.New(t)
	r.Use(t) // COCKPIT_REPO_DIR=r.Root
	c := startServe(t)
	if rep := c.call("initialize", map[string]any{"root": other.Root}); rep.refusalCode() != "binding" || !strings.Contains(rep.Error.Message, "COCKPIT_REPO_DIR") {
		t.Errorf("want a binding refusal naming COCKPIT_REPO_DIR, got %+v", rep)
	}
}

// The binary: `cockpit serve --stdio` writes protocol messages to stdout and nothing else.
func TestServe_TheBinaryKeepsStdoutForTheProtocol(t *testing.T) {
	r := testrepo.New(t)
	cmd := r.CockpitCommand(t, "serve", "--stdio")
	var in strings.Builder
	for _, m := range []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"root": r.Root}},
		{"jsonrpc": "2.0", "id": 2, "method": "doctor"},
		{"jsonrpc": "2.0", "id": 3, "method": "say/templates"},
	} {
		b, _ := json.Marshal(m)
		_ = frontrpc.WriteMessage(&in, b)
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("serve --stdio: %v", err)
	}
	rd := bufio.NewReader(strings.NewReader(string(out)))
	for want := 1; want <= 3; want++ {
		body, err := frontrpc.ReadMessage(rd)
		if err != nil {
			t.Fatalf("message %d: %v\nstdout:\n%s", want, err, out)
		}
		var rep serveReply
		if json.Unmarshal(body, &rep) != nil || rep.Error != nil {
			t.Errorf("message %d: %s", want, body)
		}
	}
	if rest, _ := io.ReadAll(rd); len(rest) != 0 {
		t.Errorf("stdout holds more than the protocol: %q", rest)
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// A say running when the client shuts down is answered, as is everything accepted before: shutdown
// waits for it, and exit after (kton-review S1 on #186). Sent at once to the binary.
func TestServe_ASayRunningAtShutdownIsAnswered(t *testing.T) {
	r := testrepo.New(t)
	cmd := r.CockpitCommand(t, "serve", "--stdio")
	var in strings.Builder
	for _, m := range []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"root": r.Root}},
		{"jsonrpc": "2.0", "id": 2, "method": "say", "params": map[string]any{"template": "working-on", "subject": "urn:example:x",
			"fields": map[string]string{"step": "analysis", "by-session": "s"}}},
		{"jsonrpc": "2.0", "id": 3, "method": "shutdown"},
		{"jsonrpc": "2.0", "method": "exit"},
	} {
		b, _ := json.Marshal(m)
		_ = frontrpc.WriteMessage(&in, b)
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("serve --stdio: %v", err)
	}
	rd := bufio.NewReader(strings.NewReader(string(out)))
	var order []string
	var said string
	for {
		body, err := frontrpc.ReadMessage(rd)
		if err != nil {
			break
		}
		var rep serveReply
		_ = json.Unmarshal(body, &rep)
		order = append(order, string(rep.ID))
		if string(rep.ID) == "2" {
			var res struct {
				ClaimID string `json:"claimId"`
			}
			_ = json.Unmarshal(rep.Result, &res)
			said = res.ClaimID
		}
	}
	if strings.Join(order, ",") != "1,2,3" || !strings.HasPrefix(said, "sha256:") {
		t.Errorf("want initialize, the say (with its claim id) and shutdown answered in that order, got %v, claim %q\n%s", order, said, out)
	}
}
