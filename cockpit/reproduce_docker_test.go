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
