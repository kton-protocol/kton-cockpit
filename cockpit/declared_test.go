package cockpit

import (
	"context"
	"errors"
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
