//go:build docker && unreleased

package cockpit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// sparqlFixture is two signers that ran the same step on the same bytes, ours with a description, a
// rationale and a claim in a scope, theirs with a description signed by a key nobody here trusts.
type sparqlFixture struct {
	ours                 *testrepo.Repo
	in, out              string // file hex
	step, theirStep      string // foton hex
	keyid                string // our claims key
	described, rationale string // claim hex
	scoped               string // claim hex
}

func hexOf(id string) string { return strings.TrimPrefix(id, "sha256:") }

func newSparqlFixture(t *testing.T) *sparqlFixture {
	t.Helper()
	ctx := context.Background()
	f := &sparqlFixture{}

	theirs := describesRepo(t)
	theirs.Write(t, "data/in.csv", "id,value\n1,42\n")
	theirs.Write(t, "data/analyse.py", "print('deterministic')\n")
	theirs.Write(t, "data/out.csv", "id,result\n1,84\n")
	res, pub, err := Publish(ctx, nil, PublishRequest{Inputs: []string{"data/in.csv", "data/analyse.py"},
		Outputs: []string{"data/out.csv"}, Cmd: "python data/analyse.py data/in.csv > data/out.csv # again"})
	if err != nil || res.IsError {
		t.Fatalf("their publish: %v %s", err, errText(res))
	}
	f.theirStep = hexOf(pub.FotonID)
	sayValue(t, pub.FotonID, "describes", "said by a key nobody here trusts")

	ours := testrepo.New(t)
	f.ours = ours
	programme := ours.SeedScope(t, "programme-cardio")
	project := ours.SeedScopeUnder(t, "project-af", programme)
	study := ours.SeedScopeUnder(t, "study-af-2026", project)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes", "rationale")
	raw.Claims.Scopes = map[string]string{"study": study}
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	must(t, json.Unmarshal(b, &spec))
	raw.Federation.Sources = []source.Spec{spec}
	key, err := os.ReadFile(filepath.Join(theirs.Root, "registry", "keys", testrepo.SessionID+".pub"))
	must(t, err)
	ours.Write(t, "registry/keys/theirs.pub", string(key))
	raw.Trust.Tiers["peer"] = []string{"registry/keys/theirs.pub"}
	raw.Query.Image = oxigraphImage
	ours.WriteConfig(t, raw)
	ours.Use(t)

	mine := publishOne(t, ours)
	f.step = hexOf(mine.FotonID)
	f.described = hexOf(sayValue(t, mine.FotonID, "describes", "dropped rows without a visit date"))
	f.rationale = hexOf(sayValue(t, mine.FotonID, "rationale", "the protocol counts visits, not patients"))
	res, said, err := Say(ctx, nil, SayRequest{Subject: mine.FotonID, Template: "working-on", Scope: "study",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID}})
	if err != nil || res.IsError {
		t.Fatalf("say in scope: %v %s", err, errText(res))
	}
	f.scoped = hexOf(said.ClaimID)

	for name, content := range map[string]*string{"id,value\n1,42\n": &f.in, "id,result\n1,84\n": &f.out} {
		sum := sha256.Sum256([]byte(name))
		*content = hex.EncodeToString(sum[:])
	}
	pk, err := loadPublic(filepath.Join(ours.Root, "registry", "keys", testrepo.SessionID+"-claims.pub"))
	must(t, err)
	f.keyid = core.KeyIDHex(pk)
	return f
}

// docExamples are the sparql blocks of docs/sparql.md under the examples heading, by the heading
// above each.
func docExamples(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "docs", "sparql.md"))
	must(t, err)
	doc := string(b)
	doc = doc[strings.Index(doc, "## Examples"):]
	out := map[string]string{}
	for _, m := range regexp.MustCompile("(?s)### ([^\n]+)\n.*?```sparql\n(.*?)```").FindAllStringSubmatch(doc, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func sparqlAsk(t *testing.T, q string) *SparqlAnswer {
	t.Helper()
	res, out, err := Ask(context.Background(), nil, AskRequest{Query: "sparql", Sparql: q})
	if err != nil || res.IsError {
		t.Fatalf("%s\n%v %s", q, err, errText(res))
	}
	return out.Sparql
}

func column(a *SparqlAnswer, v string) map[string]bool {
	out := map[string]bool{}
	for _, r := range a.Rows {
		out[r[v]] = true
	}
	return out
}

// Every example of docs/sparql.md answers what it says it answers, in the daemon and in a container
// for the query alone alike, so the examples an assistant is taught from are correct against the
// projection.
func TestSparql_TheExamplesAnswerOverAFixture(t *testing.T) {
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	f := newSparqlFixture(t)
	examples := docExamples(t)
	o := func(h string) string { return "https://kton.dev/o/" + h }
	agent := "https://kton.dev/agent/" + f.keyid

	type check func(t *testing.T, a *SparqlAnswer)
	cases := []struct {
		heading string
		fill    map[string]string
		check   check
	}{
		{"Every step that used a file, and who signed it", map[string]string{"FILE": f.in}, func(t *testing.T, a *SparqlAnswer) {
			steps, signers := column(a, "step"), column(a, "signer")
			if len(a.Rows) != 2 || !steps[o(f.step)] || !steps[o(f.theirStep)] || len(signers) != 2 || signers[""] {
				t.Errorf("want both steps, each signed by its own trusted key: %v", a.Rows)
			}
		}},
		{"The descriptions and rationales of a step", map[string]string{"STEP": f.step}, func(t *testing.T, a *SparqlAnswer) {
			got := map[string]string{}
			for _, r := range a.Rows {
				got[r["kind"]] = r["text"]
				if r["signer"] != agent || r["when"] == "" {
					t.Errorf("want our signer and a time: %v", r)
				}
			}
			if len(a.Rows) != 2 || got["description"] != "dropped rows without a visit date" ||
				got["rationale"] != "the protocol counts visits, not patients" {
				t.Errorf("want the description and the rationale: %v", a.Rows)
			}
		}},
		{"What one signer said in a time window", map[string]string{"KEYID": f.keyid}, func(t *testing.T, a *SparqlAnswer) {
			claims := column(a, "claim")
			if !claims[o(f.described)] || !claims[o(f.rationale)] || !claims[o(f.scoped)] {
				t.Errorf("want our claims: %v", a.Rows)
			}
		}},
		{"The claims in a scope, by its name", nil, func(t *testing.T, a *SparqlAnswer) {
			if len(a.Rows) != 1 || a.Rows[0]["claim"] != o(f.scoped) || a.Rows[0]["subject"] != o(f.step) ||
				a.Rows[0]["predicate"] != "https://kton.dev/v/working-on" {
				t.Errorf("want the one claim chained in the study: %v", a.Rows)
			}
		}},
		{"Files that two independent signers produced", nil, func(t *testing.T, a *SparqlAnswer) {
			if len(a.Rows) != 1 || a.Rows[0]["file"] != o(f.out) || a.Rows[0]["path"] != "data/out.csv" {
				t.Errorf("want out.csv: %v", a.Rows)
			}
		}},
		{"Steps that ran a script, by its path", nil, func(t *testing.T, a *SparqlAnswer) {
			steps := column(a, "step")
			if len(steps) != 2 || !steps[o(f.step)] || !steps[o(f.theirStep)] {
				t.Errorf("want both steps: %v", a.Rows)
			}
		}},
		{"Everything a file was derived from", map[string]string{"FILE": f.out}, func(t *testing.T, a *SparqlAnswer) {
			paths := column(a, "path")
			if len(column(a, "source")) != 2 || !paths["data/in.csv"] || !paths["data/analyse.py"] {
				t.Errorf("want in.csv and analyse.py: %v", a.Rows)
			}
		}},
		{"Did a key you trust produce this file?", map[string]string{"FILE": f.out}, func(t *testing.T, a *SparqlAnswer) {
			if a.Form != "ASK" || a.Boolean == nil || !*a.Boolean {
				t.Errorf("want true: %+v", a)
			}
		}},
	}
	if len(cases) != len(examples) {
		t.Fatalf("docs/sparql.md has %d examples, the test checks %d", len(examples), len(cases))
	}

	inContainer := map[string]string{}
	for _, path := range []string{"container", "index"} {
		if path == "index" {
			raw := f.ours.Config(t).Raw
			raw.Index.Kind = "oxigraph"
			f.ours.WriteConfig(t, raw)
			cfg := f.ours.Config(t)
			t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
		}
		for _, c := range cases {
			q, ok := examples[c.heading]
			if !ok {
				t.Fatalf("docs/sparql.md has no example %q", c.heading)
			}
			for k, v := range c.fill {
				q = strings.ReplaceAll(q, k, v)
			}
			t.Run(path+"/"+c.heading, func(t *testing.T) {
				a := sparqlAsk(t, q)
				if path == "index" && !strings.HasPrefix(a.Via, "index oxigraph") {
					t.Errorf("not answered by the index: %s", a.Via)
				}
				c.check(t, a)
				rows := fmt.Sprint(sorted(a.Rows), a.Boolean != nil && *a.Boolean)
				if path == "container" {
					inContainer[c.heading] = rows
				} else if rows != inContainer[c.heading] {
					t.Errorf("the index answered otherwise than a container:\nindex     %s\ncontainer %s", rows, inContainer[c.heading])
				}
			})
		}
	}

	// A claim nobody here trusts is not in the dataset, whatever a query asks for.
	a := sparqlAsk(t, `SELECT ?text { GRAPH ?g { ?s <http://purl.org/dc/terms/description> ?o . ?o <https://kton.dev/v/lab/value> ?text } }`)
	if texts := column(a, "text"); texts["said by a key nobody here trusts"] || len(texts) != 1 {
		t.Errorf("want our description only: %v", a.Rows)
	}
}
