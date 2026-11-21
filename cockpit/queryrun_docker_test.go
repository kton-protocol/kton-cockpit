//go:build docker

package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"kton.dev/plankton/blobstore"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A reproducible query is a query run (ADR-020): recorded as a query/sparql foton whose query,
// manifest and canonical result are in the content store; its manifest pins the scope's tip and the
// trust keys; a second run reads a dataset that holds the first, with the same result; a value of the
// moment is refused.
func TestQueryRun_IsARecordWithItsManifest(t *testing.T) {
	r := testrepo.New(t)
	study := r.SeedScope(t, "study")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	raw.Claims.Scopes = map[string]string{"study": study}
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)
	ctx := context.Background()
	var said SayResult
	for _, v := range []string{"zero", "one"} {
		res, out, err := Say(ctx, nil, SayRequest{Subject: "urn:example:x", Template: "describes", Scope: "study",
			Fields: map[string]string{"value": v}})
		if err != nil || res.IsError {
			t.Fatalf("say: %v %s", err, errText(res))
		}
		said = out // the second: the scope's one tip
	}
	q := `PREFIX nk: <https://kton.dev/v/>
SELECT ?c ?v WHERE { ?c nk:about ?s ; <https://kton.dev/v/lab/value> ?v }`
	ask := func() *SparqlAnswer {
		t.Helper()
		a, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Sparql: q, Reproducible: true})
		if err != nil {
			t.Fatal(err)
		}
		return a.Sparql
	}
	// A claim signed after the run pinned what it reads is in neither its rows nor its manifest
	// (kton-review Q1 on #225).
	afterPin = func() {
		afterPin = nil
		if res, _, err := Say(ctx, nil, SayRequest{Subject: "urn:example:x", Template: "describes", Scope: "study",
			Fields: map[string]string{"value": "two"}}); err != nil || res.IsError {
			t.Errorf("say during the run: %v %s", err, errText(res))
		}
	}
	a := ask()
	if a.Run == nil || a.Run.FotonID == "" || len(a.Rows) != 2 {
		t.Fatalf("a query run: want a record and two rows, got %+v", a)
	}
	f, err := binaries.New(r.Config(t)).Foton(ctx, a.Run.FotonID)
	if err != nil || f.Protocol.Kind != "query/sparql" {
		t.Fatalf("the run's record: %+v %v", f, err)
	}
	store, err := blobstore.Open(r.Config(t).BlobsDir)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := store.Get(a.Run.Manifest)
	if err != nil {
		t.Fatalf("the manifest is in the content store: %v", err)
	}
	var m QueryManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	seed := study
	if tips := m.Scopes[seed]; len(tips) != 1 || tips[0] != said.ClaimID {
		t.Errorf("the manifest pins the study at its tip, the claim said: %v (want %s)", m.Scopes, said.ClaimID)
	}
	for k := range m.Scopes {
		if !strings.HasPrefix(k, "sha256:") {
			t.Errorf("the manifest names a scope by its id, not %q", k)
		}
	}
	if len(m.Trust) == 0 || m.Templates == "" || m.ViewVersion == 0 || m.Engine != oxigraphImage || len(m.Registries) == 0 {
		t.Errorf("the manifest pins trust, vocabulary, projection and engine: %+v", m)
	}
	for _, h := range []string{a.Run.Query, a.Run.Result, m.Templates, m.Registries[0].Claims} {
		if !store.Has(h) {
			t.Errorf("%s is not in the content store", h)
		}
	}
	// Run again: the claim signed during the first run is in this one, and so is the first run's record:
	// another record, three rows.
	again := ask()
	if len(again.Rows) != 3 || again.Run.FotonID == a.Run.FotonID {
		t.Errorf("a second run: want three rows in another record: %d rows, %+v", len(again.Rows), again.Run)
	}

	_, err = New(Start{}).Ask(ctx, AskRequest{Query: "sparql", Reproducible: true,
		Sparql: `SELECT ?t WHERE { BIND(NOW() AS ?t) }`})
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Code != "query.not-reproducible" {
		t.Errorf("NOW() in a query run: want query.not-reproducible, got %v", err)
	}
}
