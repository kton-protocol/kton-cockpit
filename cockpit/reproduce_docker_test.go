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

	out, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID})
	if err != nil {
		t.Fatalf("reproduce: %v", err)
	}
	if out.FotonID != run.FotonID || out.Result != run.Result || out.Rows != rows {
		t.Errorf("a match signs the run's own record with its result: %+v (run %+v, %d rows)", out, run, rows)
	}
	notes := strings.Join(out.Notes, "\n")
	for _, want := range []string{"no longer trusts", "vocabulary"} {
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
	if _, err := New(Start{}).Reproduce(ctx, ReproduceRequest{Run: run.FotonID}); err != nil {
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
