package cockpit

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A step declared afterwards (hand, or heuristic:<name>) says so in its signed record, and is another
// record than the same step observed; a malformed value, or one with outputs only a run finds, is
// refused (MH, 21.11.; cockpit-runtime's story editor).
func TestPublish_ADeclaredStepSaysSoInItsRecord(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	publish := func(declared string) (*PublishResult, error) {
		return New(Start{}).Publish(ctx, PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
			Cmd: "python make.py", Declared: declared})
	}
	declared, err := publish("hand")
	if err != nil {
		t.Fatal(err)
	}
	f, err := binaries.New(r.Config(t)).Foton(ctx, declared.FotonID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Descriptor["declared"] != "hand" {
		t.Errorf("the signed record says declared hand: %v", f.Protocol.Descriptor)
	}
	observed, err := publish("")
	if err != nil {
		t.Fatal(err)
	}
	if observed.FotonID == declared.FotonID {
		t.Error("the same step observed is another record than declared")
	}
	if g, _ := binaries.New(r.Config(t)).Foton(ctx, observed.FotonID); g == nil || g.Protocol.Descriptor["declared"] != nil {
		t.Errorf("an observed step carries no declared: %+v", g)
	}
	if _, err := publish("heuristic:story-join"); err != nil {
		t.Errorf("heuristic:<name>: %v", err)
	}
	var ref *Refusal
	for _, bad := range []string{"by hand", "heuristic:", "observed"} {
		if _, err := publish(bad); !errors.As(err, &ref) || ref.Code != "argument" {
			t.Errorf("declared %q: want refused, got %v", bad, err)
		}
	}
	_, err = New(Start{}).Publish(ctx, PublishRequest{Inputs: []string{"data/in.csv"}, OutputsIn: "data", Cmd: "x", Declared: "hand"})
	if !errors.As(err, &ref) || ref.Code != "argument" {
		t.Errorf("declared with outputsIn: want refused, got %v", err)
	}
}

// A declared step is no reproduction: of an observed and a declared step producing the same bytes, the
// count keeps the observed one and says it left the declared one out (kton-review on #220).
func TestReproductions_ADeclaredStepIsNoReproduction(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	for _, declared := range []string{"", "hand"} {
		if _, err := New(Start{}).Publish(ctx, PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
			Cmd: "python make.py", Declared: declared}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := binaries.New(r.Config(t)).HashFile(ctx, filepath.Join(r.Root, "data/out.csv"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := binaries.New(r.Config(t)).Reproductions(ctx, out, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.ProducerFotons != 1 || res.ExcludedDeclared != 1 || res.DistinctSigners != 1 {
		t.Errorf("want the observed producer only, the declared one excluded: %+v", res)
	}
}
