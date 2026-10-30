//go:build docker

package cockpit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A vocabulary package brings words and questions: once installed and allowed, say signs with its
// templates and ask sparql runs its queries by name, over the same projection as a query written
// out, with variables bound as literals (SPEC §8.1 and SPEC §9.9).
func TestVocabulary_AnAllowedPackageBringsItsTemplatesAndQueries(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)

	src := t.TempDir()
	write := func(p, s string) { must(t, os.MkdirAll(filepath.Dir(p), 0o755)); must(t, os.WriteFile(p, []byte(s), 0o644)) }
	write(filepath.Join(src, "templates", "requests.json"), `{"name": "requests", "target": "either",
  "predicate": "https://example.org/ln/requests", "fields": {"lane": {"type": "string", "required": true}}}`)
	write(filepath.Join(src, "queries", "open.rq"), `PREFIX nk: <https://kton.dev/v/>
SELECT ?msg WHERE { ?msg nk:predicate <https://example.org/ln/requests> ; <https://kton.dev/v/lab/lane> $LANE }`)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	pkg := filepath.Join(t.TempDir(), "inbox")
	if _, _, err := scope.BuildTemplates(pkg, "inbox", "v1", filepath.Join(src, "templates"), filepath.Join(src, "queries"), key, scope.Options{}); err != nil {
		t.Fatal(err)
	}

	c := New(Start{})
	if _, err := c.Ask(ctx, AskRequest{Query: "sparql", Name: "open"}); err == nil {
		t.Fatal("before the package is allowed, its query must be unknown")
	}
	inst, err := c.Install(ctx, InstallRequest{Path: pkg, Allow: true})
	if err != nil || !inst.Allowed {
		t.Fatalf("install --allow: %+v %v", inst, err)
	}

	subject := publishOne(t, r).FotonID
	said := map[string]string{}
	for _, lane := range []string{"kton-cockpit", "kton"} {
		res, err := c.Say(ctx, SayRequest{Subject: subject, Template: "requests", Fields: map[string]string{"lane": lane}})
		if err != nil {
			t.Fatalf("say with the package's template: %v", err)
		}
		said[lane] = res.ClaimID
	}

	out, err := c.Ask(ctx, AskRequest{Query: "sparql", Name: "open", Bind: map[string]string{"LANE": "kton-cockpit"}})
	if err != nil {
		t.Fatalf("ask sparql --name open: %v", err)
	}
	if out.Sparql.From == "" || len(out.Sparql.Rows) != 1 || out.Sparql.Rows[0]["msg"] != "https://kton.dev/o/"+hexOf(said["kton-cockpit"]) {
		t.Fatalf("want the one request for kton-cockpit, from the package: %+v", out.Sparql)
	}

	// A bound value is a literal, never query text.
	out, err = c.Ask(ctx, AskRequest{Query: "sparql", Name: "open", Bind: map[string]string{"LANE": `x" } UNION { ?msg ?p ?o } #`}})
	if err != nil || len(out.Sparql.Rows) != 0 {
		t.Fatalf("a value that looks like SPARQL must match nothing: %+v %v", out, err)
	}
	if _, err := c.Ask(ctx, AskRequest{Query: "sparql", Name: "open", Bind: map[string]string{"LANE x": "y"}}); err == nil {
		t.Fatal("a binding to something that is not a variable name must be refused")
	}
}
