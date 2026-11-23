//go:build docker

package cockpit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A query run is reproduced from the records (ADR-020, section 4): a claim added to its scope since,
// and one on another branch of the scope, are not in the reproduction; a template added and a key no
// longer trusted since leave it matching and are said beside it; a reproduction by another key counts
// in the run's ↻N; a pinned byte that cannot be found refuses it, named.
func TestReproduce_QueryRun(t *testing.T) {
	r := testrepo.New(t)
	study := r.SeedScope(t, "study")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study": study}
	raw.Query.Image = oxigraphImage
	cfg := r.Config(t)
	cfg.Raw = raw
	// A key trusted when the run is made, and no longer when it is reproduced.
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	revoked := testrepo.TrustPub(t, cfg, "partners", "partner", hex.EncodeToString(pub))
	r.WriteConfig(t, cfg.Raw)
	r.Use(t)
	ctx := context.Background()
	say := func(v string) SayResult {
		t.Helper()
		res, out, err := Say(ctx, nil, SayRequest{Subject: "urn:example:x", Template: "describes", Scope: "study",
			Fields: map[string]string{"value": v}})
		if err != nil || res.IsError {
			t.Fatalf("say: %v %s", err, errText(res))
		}
		return out
	}
	first := say("zero")
	say("one")
	q := `PREFIX nk: <https://kton.dev/v/>
SELECT ?c ?v WHERE { ?c nk:about ?s OPTIONAL { ?c <https://kton.dev/v/lab/value> ?v } }`
	a, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	run := a.Sparql.Run
	if run == nil || len(a.Sparql.Rows) == 0 {
		t.Fatalf("a query run with rows: %+v", a.Sparql)
	}
	rows := len(a.Sparql.Rows)

	// Since the run: a claim at the scope's tip, one on another branch of it, a template, a key revoked.
	say("two")
	r.ChainClaimDirectly(t, "urn:example:y", study, first.ClaimID, "fork")
	r.Write(t, filepath.Join(config.Dir, "templates", "noted.json"),
		`{"name":"noted","target":"either","predicate":"https://example.org/noted","fields":{"note":{"type":"string"}}}`)
	// The field the query reads now names another term: the reproduction reads it with the pinned view.
	r.Write(t, filepath.Join(config.Dir, "aliases.json"), `{"terms":{"value":"https://example.org/value"}}`)
	if err := os.Remove(revoked); err != nil {
		t.Fatal(err)
	}
	raw.Trust.Tiers = []string{"self"}
	r.WriteConfig(t, raw)

	// It rests on a key not trusted here: reproducible, not signed (kton-review R1 on #229).
	out, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	if err != nil {
		t.Fatalf("reproduce: %v", err)
	}
	if !out.Reproducible || out.Signed || out.FotonID != "" || !strings.Contains(out.NotSigned, "not trusted here") || out.Result != run.Result {
		t.Errorf("a match resting on a key not trusted here is reproducible and not signed: %+v", out)
	}
	notes := strings.Join(out.Notes, "\n")
	out, err = New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID, SignAnyway: true})
	if err != nil {
		t.Fatalf("reproduce, signing anyway: %v", err)
	}
	if !out.Signed || out.FotonID != run.FotonID || out.Result != run.Result || out.Rows != rows {
		t.Errorf("a match signed when asked signs the run's own record with its result: %+v (run %+v, %d rows)", out, run, rows)
	}
	for _, want := range []string{"not trusted here", "vocabulary"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the reproduction says what differs from the present (%q): %v", want, out.Notes)
		}
	}

	// Another key reproduces it: the run's ↻N counts two signers.
	keys := filepath.Join(r.Keys)
	if b, err := exec.Command(filepath.Join(r.Bin, "plankton"), "keygen", filepath.Join(keys, "other")).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v %s", err, b)
	}
	ob, err := os.ReadFile(filepath.Join(keys, "other.pub"))
	if err != nil {
		t.Fatal(err)
	}
	cfg = r.Config(t)
	testrepo.TrustPub(t, cfg, "self", "other", string(ob))
	r.WriteIdentity(t, config.Identity{SessionID: testrepo.SessionID, PlanktonKey: filepath.Join(keys, "other.key"),
		NektonKey: r.Key(testrepo.SessionID + "-claims")})
	if _, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID, SignAnyway: true}); err != nil {
		t.Fatalf("reproduce by another key: %v", err)
	}
	res, err := New(Start{}).Ask(ctx, AskRequest{Query: "reproductions", Ref: run.Result})
	if err != nil {
		t.Fatal(err)
	}
	if res.VerifiedSigners != 2 {
		t.Errorf("the run's ↻N counts the reproducer: %d signers (%s)", res.VerifiedSigners, res.Raw)
	}

	// A pinned byte that is gone refuses the reproduction, named.
	store, err := blobstore.Open(r.Config(t).BlobsDir)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := store.Get(run.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m QueryManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	h := strings.TrimPrefix(m.Registries[0].Claims, "sha256:")
	if err := os.Remove(filepath.Join(r.Config(t).BlobsDir, h[:2], h)); err != nil {
		t.Fatal(err)
	}
	_, err = New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Code != "reproduce.missing" || !strings.Contains(ref.Reason, m.Registries[0].Claims) {
		t.Errorf("a pinned byte that is gone: want reproduce.missing naming it, got %v", err)
	}
	// Not a query run: refused as such.
	if _, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: "sha256:" + strings.Repeat("0", 64)}); !errors.As(err, &ref) || ref.Code != "reproduce.missing" {
		t.Errorf("an id in no registry: want reproduce.missing, got %v", err)
	}
}

// A run made in another repository, with keys this one does not trust, is reproducible here and not
// signed: a co-signature would count in its ↻N as an independent reproduction by a key others may trust
// (kton-review R1 on #229, probe zz229). A run whose pinned records are in no registry read any more is
// refused (the completeness check), and one whose engine is not on the machine (never pulled).
func TestReproduce_ARunOnKeysNotTrustedHereIsNotSigned(t *testing.T) {
	ctx := context.Background()
	theirs := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Query.Image = oxigraphImage
	theirs.WriteConfig(t, raw)
	theirs.Use(t)
	for _, v := range []string{"a", "b"} {
		if _, err := New(Start{}).Say(ctx, SayRequest{Subject: "urn:example:x", Template: "describes", Fields: map[string]string{"value": v}}); err != nil {
			t.Fatal(err)
		}
	}
	q := `SELECT ?c ?v WHERE { ?c <https://kton.dev/v/lab/value> ?v }`
	a, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err != nil || a.Sparql.Run == nil || len(a.Sparql.Rows) != 2 {
		t.Fatalf("their run: %v %+v", err, a)
	}
	theirRun := a.Sparql.Run

	ours := testrepo.New(t)
	oraw := testrepo.DefaultConfig()
	oraw.Query.Image = oxigraphImage
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	oraw.Federation.Sources = []source.Spec{spec}
	ours.WriteConfig(t, oraw)
	ours.Use(t)
	out, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: theirRun.FotonID})
	if err != nil {
		t.Fatalf("reproduce their run here: %v", err)
	}
	if !out.Reproducible || out.Signed || out.FotonID != "" || out.NotSigned == "" {
		t.Errorf("their run on keys not trusted here: reproducible, not signed: %+v", out)
	}
	if !strings.Contains(strings.Join(out.Notes, "\n"), "signed by no key trusted here") {
		t.Errorf("the note names the run's signer as not trusted here: %v", out.Notes)
	}
	res, err := New(Start{}).Ask(ctx, AskRequest{Query: "reproductions", Ref: theirRun.Result})
	if err == nil && res.VerifiedSigners > 1 {
		t.Errorf("nothing was co-signed: %d signers", res.VerifiedSigners)
	}

	// Our own run over the union, then the source taken out: its pinned records are in no registry
	// read, and the reproduction is refused, naming the first.
	a, err = New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err != nil || a.Sparql.Run == nil {
		t.Fatalf("our run over the union: %v %+v", err, a)
	}
	ourRun := a.Sparql.Run
	// The engine is never pulled: absent, refused, named.
	enginePresent = func(context.Context, *config.Config, string) bool { return false }
	_, err = New(Start{}).Reproduce(ctx, ReproduceRequest{Run: ourRun.FotonID})
	enginePresent = imagePresent
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Code != "reproduce.unsupported" || !strings.Contains(ref.Reason, oxigraphImage) {
		t.Errorf("an engine not on the machine: want reproduce.unsupported naming it, got %v", err)
	}
	oraw.Federation.Sources = nil
	ours.WriteConfig(t, oraw)
	_, err = New(Start{}).Reproduce(ctx, ReproduceRequest{Run: ourRun.FotonID})
	if !errors.As(err, &ref) || ref.Code != "query.not-reproducible" || !strings.Contains(ref.Reason, "sha256:") {
		t.Errorf("pinned records in no registry read: want query.not-reproducible naming the first, got %v", err)
	}
}

// A run signed by a key this repository does not trust is not signed here, even when every key its trust
// pinned is trusted: steps are in the dataset whoever signed them (kton-review's note on #229, MH 23.11.).
// Asked for, it is signed.
func TestReproduce_ARunByAnUntrustedSignerIsNotSigned(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)
	if _, err := New(Start{}).Say(ctx, SayRequest{Subject: "urn:example:x", Template: "describes", Fields: map[string]string{"value": "a"}}); err != nil {
		t.Fatal(err)
	}
	// The run is signed by a key in no tier here; the trust it pins is this repository's own.
	if b, err := exec.Command(filepath.Join(r.Bin, "plankton"), "keygen", filepath.Join(r.Keys, "unknown")).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v %s", err, b)
	}
	r.WriteIdentity(t, config.Identity{SessionID: testrepo.SessionID, PlanktonKey: filepath.Join(r.Keys, "unknown.key"),
		NektonKey: r.Key(testrepo.SessionID + "-claims")})
	a, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Reproducible: true,
		Sparql: `SELECT ?c ?v WHERE { ?c <https://kton.dev/v/lab/value> ?v }`})
	if err != nil || a.Sparql.Run == nil {
		t.Fatalf("the run: %v %+v", err, a)
	}
	run := a.Sparql.Run
	r.WriteIdentity(t, config.Identity{SessionID: testrepo.SessionID, PlanktonKey: r.Key(testrepo.SessionID),
		NektonKey: r.Key(testrepo.SessionID + "-claims")})
	out, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Reproducible || out.Signed || !strings.Contains(out.NotSigned, "signed by no key trusted here") || strings.Contains(out.NotSigned, "rests on") {
		t.Errorf("a run by an untrusted signer over trusted pins: reproducible, not signed, for its signer: %+v", out)
	}
	out, err = New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID, SignAnyway: true})
	if err != nil || !out.Signed || out.FotonID != run.FotonID {
		t.Errorf("signed when asked: %+v %v", out, err)
	}
}

// A loaded file is an input of a query run (ADR-020 section 2): pinned by its hash, its bytes in the content
// store, read as the graph its hash names. The reproduction reads the bytes from the store, whatever the file
// on disk says since; with them gone, it is refused, named. A run over a loaded file whose bytes changed on
// disk since its load is refused, named.
func TestQueryRun_ALoadedFileIsAnInput(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	r.Write(t, "rdf/coverage.ttl", turtle)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/coverage.ttl"}}); err != nil {
		t.Fatal(err)
	}
	h := core.HashBytes([]byte(turtle))
	q := `SELECT ?e ?o WHERE { GRAPH ?g { ?e <https://example.org/outcome> ?o } }`
	a, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	run := a.Sparql.Run
	if run == nil || len(a.Sparql.Rows) != 2 || a.Sparql.LoadedGraphs != 1 {
		t.Fatalf("a query run reads the loaded graph, and says so: %+v", a.Sparql)
	}
	cfg := r.Config(t)
	store, err := blobstore.Open(cfg.BlobsDir)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := store.Get(run.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var m QueryManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Loaded) != 1 || m.Loaded[0].Hash != h || m.Loaded[0].MediaType != "text/turtle" || !store.Has(h) {
		t.Fatalf("the manifest pins the loaded file by its hash, its bytes in the store: %+v", m.Loaded)
	}
	if m.Version != 3 {
		t.Errorf("a run that read loaded files is manifest version 3, which a cockpit reading only 2 refuses: %d", m.Version)
	}
	// --graph names the loaded file by its hash: it is pinned, and named in the manifest.
	if n, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true, Graphs: []string{h}}); err != nil || n.Sparql.LoadedGraphs != 1 || len(n.Sparql.Rows) != 2 {
		t.Errorf("--graph %s: the loaded graph read: %+v %v", h, n, err)
	}
	// The reproducer's own limits apply to the pinned bytes (kton-review L1 on #235).
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	raw.Index.Kind = "oxigraph"
	raw.Index.Derived.Enabled = true
	raw.Index.Derived.MaxBytes = 10
	r.WriteConfig(t, raw)
	if _, err := c.Reproduce(ctx, ReproduceRequest{Run: run.FotonID}); codeOf(err) != "derived.refused" || !strings.Contains(err.Error(), h) {
		t.Errorf("pinned bytes over the reproducer's maxBytes: want derived.refused naming %s, got %v", h, err)
	}
	raw.Index.Derived.MaxBytes = 0
	raw.Index.Derived.MaxTriples = 1
	r.WriteConfig(t, raw)
	if _, err := c.Reproduce(ctx, ReproduceRequest{Run: run.FotonID}); codeOf(err) != "derived.refused" || !strings.Contains(err.Error(), "maxTriples") {
		t.Errorf("pinned graphs over the reproducer's maxTriples: want derived.refused, got %v", err)
	}
	raw.Index.Derived.MaxTriples = 0
	r.WriteConfig(t, raw)
	// The file on disk changes: the reproduction reads the pinned bytes.
	r.Write(t, "rdf/coverage.ttl", turtle+"<https://example.org/element3> <https://example.org/outcome> \"Gap\" .\n")
	out, err := c.Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	if err != nil || out.Result != run.Result || out.Rows != 2 {
		t.Errorf("reproduced from the stored bytes: %+v %v", out, err)
	}
	// A new run over the changed file: refused, named, not run over bytes no store holds.
	_, err = c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err == nil || !strings.Contains(err.Error(), h) {
		t.Errorf("a loaded file changed since its load: want a refusal naming %s, got %v", h, err)
	}
	// The pinned bytes gone: the reproduction is refused, naming them.
	hx := strings.TrimPrefix(h, "sha256:")
	if err := os.Remove(filepath.Join(cfg.BlobsDir, hx[:2], hx)); err != nil {
		t.Fatal(err)
	}
	_, err = c.Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Code != "reproduce.missing" || !strings.Contains(ref.Reason, h) {
		t.Errorf("the loaded bytes gone: want reproduce.missing naming %s, got %v", h, err)
	}
}

// --graph narrows a query run to the scopes named (by name or seed) and the loaded files named (by hash):
// another scope's claims are in neither its rows nor its manifest; unscoped claims stay in; the
// reproduction matches. A graph that is no scope or loaded file here is refused, as is graphs without a
// query run (ADR-020, section 1).
func TestQueryRun_GraphNarrowsTheRun(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	a := r.SeedScope(t, "study-a")
	b := r.SeedScope(t, "study-b")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study-a": a, "study-b": b}
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)
	c := New(Start{})
	for _, x := range []struct{ scope, v string }{{"study-a", "in-a"}, {"study-b", "in-b"}, {"", "unscoped"}} {
		if _, err := c.Say(ctx, SayRequest{Subject: "urn:example:x", Template: "describes", Scope: x.scope, Fields: map[string]string{"value": x.v}}); err != nil {
			t.Fatal(err)
		}
	}
	q := `SELECT ?v WHERE { ?c <https://kton.dev/v/lab/value> ?v }`
	for _, g := range []string{"study-a", a} {
		ans, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true, Graphs: []string{g}})
		if err != nil {
			t.Fatalf("--graph %s: %v", g, err)
		}
		var got []string
		for _, row := range ans.Sparql.Rows {
			got = append(got, row["v"])
		}
		if strings.Join(got, ",") != "in-a,unscoped" && strings.Join(got, ",") != "unscoped,in-a" {
			t.Errorf("--graph %s: study-a's claim and the unscoped one, not study-b's: %v", g, got)
		}
		store, _ := blobstore.Open(r.Config(t).BlobsDir)
		mb, err := store.Get(ans.Sparql.Run.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		var m QueryManifest
		if err := json.Unmarshal(mb, &m); err != nil {
			t.Fatal(err)
		}
		if len(m.Graphs) != 1 || m.Graphs[0] != a || len(m.Scopes) != 1 || m.Scopes[a] == nil {
			t.Errorf("the manifest names the graph narrowed to, and pins that scope alone: graphs %v, scopes %v", m.Graphs, m.Scopes)
		}
		if out, err := c.Reproduce(ctx, ReproduceRequest{Run: ans.Sparql.Run.FotonID}); err != nil || out.Result != ans.Sparql.Run.Result {
			t.Errorf("the narrowed run reproduces: %+v %v", out, err)
		}
	}
	var ref *Refusal
	for _, bad := range [][]string{{"no-such-scope"}, {"sha256:" + strings.Repeat("0", 64)}} {
		if _, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true, Graphs: bad}); !errors.As(err, &ref) || ref.Code != "argument" {
			t.Errorf("--graph %v: want refused, got %v", bad, err)
		}
	}
	if _, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Graphs: []string{"study-a"}}); !errors.As(err, &ref) || ref.Code != "argument" {
		t.Errorf("graphs without a query run: want refused, got %v", err)
	}
}

// A loaded file a load would refuse is refused by a query run too, whoever listed it: RDF/XML with
// entities, a billion-laughs shape (kton-review L1 on #235, probe zz235).
func TestQueryRun_ALoadedFileALoadWouldRefuseIsRefused(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	bomb := `<?xml version="1.0"?>
<!DOCTYPE rdf:RDF [ <!ENTITY a "aaaaaaaaaa"> <!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;"> ]>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:ex="https://example.org/">
  <rdf:Description rdf:about="https://example.org/x"><ex:p>&b;</ex:p></rdf:Description>
</rdf:RDF>
`
	r.Write(t, "rdf/bomb.rdf", bomb)
	h := core.HashBytes([]byte(bomb))
	// Listed by hand, as another repository's run may list it: no load of this cockpit took it.
	if err := rememberLoad(r.Config(t), loadEntry{Hash: h, From: "rdf/bomb.rdf", MediaType: "application/rdf+xml"}); err != nil {
		t.Fatal(err)
	}
	_, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Reproducible: true,
		Sparql: `SELECT ?o WHERE { GRAPH ?g { ?s ?p ?o } }`})
	if codeOf(err) != "derived.refused" || !strings.Contains(err.Error(), h) {
		t.Errorf("an entity file on the load list: want derived.refused naming %s, got %v", h, err)
	}
	// The same file in UTF-16, which a byte search for <!ENTITY does not see: refused by the check itself,
	// not left to the engine.
	u16 := []byte{0xFF, 0xFE}
	for _, r := range bomb {
		u16 = append(u16, byte(r), 0)
	}
	r.Write(t, "rdf/bomb16.rdf", string(u16))
	h16 := core.HashBytes(u16)
	if err := forgetLoad(r.Config(t), h); err != nil {
		t.Fatal(err)
	}
	if err := rememberLoad(r.Config(t), loadEntry{Hash: h16, From: "rdf/bomb16.rdf", MediaType: "application/rdf+xml"}); err != nil {
		t.Fatal(err)
	}
	_, err = New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Reproducible: true,
		Sparql: `SELECT ?o WHERE { GRAPH ?g { ?s ?p ?o } }`})
	if codeOf(err) != "derived.refused" || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("a UTF-16 entity file: want derived.refused by the check (not UTF-8), got %v", err)
	}
	// Without its BOM, ASCII in UTF-16 is valid UTF-8 with NULs between: refused by the check too.
	nobom := u16[2:]
	r.Write(t, "rdf/bomb16n.rdf", string(nobom))
	hn := core.HashBytes(nobom)
	if err := forgetLoad(r.Config(t), h16); err != nil {
		t.Fatal(err)
	}
	if err := rememberLoad(r.Config(t), loadEntry{Hash: hn, From: "rdf/bomb16n.rdf", MediaType: "application/rdf+xml"}); err != nil {
		t.Fatal(err)
	}
	_, err = New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Reproducible: true,
		Sparql: `SELECT ?o WHERE { GRAPH ?g { ?s ?p ?o } }`})
	if codeOf(err) != "derived.refused" || !strings.Contains(err.Error(), "NUL") {
		t.Errorf("a UTF-16 entity file without a BOM: want derived.refused by the check, got %v", err)
	}
}

// A file with relative IRIs gives the same triples in a query run as in a query of the index: both read
// it with the graph's IRI as the base (kton-review's point 1 on #235).
func TestQueryRun_RelativeIRIsResolveAsInTheIndex(t *testing.T) {
	r := loadingRepo(t)
	ctx := context.Background()
	c := New(Start{})
	ttl := "<item1> <https://example.org/outcome> \"Covered\" .\n"
	r.Write(t, "rdf/rel.ttl", ttl)
	if _, err := c.Load(ctx, LoadRequest{Files: []string{"rdf/rel.ttl"}}); err != nil {
		t.Fatal(err)
	}
	q := `SELECT ?s WHERE { GRAPH ?g { ?s <https://example.org/outcome> ?o } }`
	idx, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Sparql.Rows) != 1 || len(run.Sparql.Rows) != 1 || idx.Sparql.Rows[0]["s"] != run.Sparql.Rows[0]["s"] {
		t.Errorf("a relative IRI resolves alike: index %v, run %v", idx.Sparql.Rows, run.Sparql.Rows)
	}
}
