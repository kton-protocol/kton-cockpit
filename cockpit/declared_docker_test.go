//go:build docker

package cockpit

import (
	"context"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// Where the cockpit runs every command (execution.image), a declared step is still not run: it is
// recorded as declared. The same command, observed, is run and fails (MH, 21.11.).
func TestContainer_ADeclaredStepIsNotRun(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	req := PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "exit 3"}
	if _, err := New(Start{}).Publish(context.Background(), req); err == nil {
		t.Fatal("observed, the command runs and fails")
	}
	req.Declared = "hand"
	out, err := New(Start{}).Publish(context.Background(), req)
	if err != nil || out.FotonID == "" {
		t.Errorf("declared, the step is recorded without running: %v", err)
	}
}

// A declared step records no environment it did not run in: not the image this repository runs
// commands in (kton-review D1 on #220).
func TestContainer_ADeclaredStepRecordsNoImageItDidNotRunIn(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	out, err := New(Start{}).Publish(context.Background(), PublishRequest{Inputs: []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"}, Cmd: "exit 3", Declared: "hand"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := binaries.New(r.Config(t)).Foton(context.Background(), out.FotonID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Descriptor["envRef"] != nil || out.Environment != "" {
		t.Errorf("a declared step names no image it ran in: %v (environment %q)", f.Protocol.Descriptor, out.Environment)
	}
}

// A query tells a declared step from an observed one (kton v0.4.3, kton#292): the declared one carries
// nk:declared with what declared it, the observed one none (absent is "not declared").
func TestSparql_ADeclaredStepCarriesNkDeclared(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Query.Image = oxigraphImage
	r.WriteConfig(t, raw)
	r.Use(t)
	ctx := context.Background()
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	ids := map[string]string{}
	for _, d := range []string{"hand", ""} {
		out, err := New(Start{}).Publish(ctx, PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
			Cmd: "python make.py", Declared: d})
		if err != nil {
			t.Fatal(err)
		}
		ids[d] = out.FotonID
	}
	a, err := New(Start{}).Ask(ctx, AskRequest{Query: "sparql",
		Sparql: `SELECT ?s ?d WHERE { ?s <https://kton.dev/v/declared> ?d }`})
	if err != nil {
		t.Fatal(err)
	}
	rows := a.Sparql.Rows
	if len(rows) != 1 || !strings.HasSuffix(rows[0]["s"], strings.TrimPrefix(ids["hand"], "sha256:")) || rows[0]["d"] != "hand" {
		t.Errorf("one step declared hand, the observed one not: %v (declared %s, observed %s)", rows, ids["hand"], ids[""])
	}
}
