package cockpit

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

func askProducer(t *testing.T, hash string) *AskResult {
	t.Helper()
	result, out, err := Ask(context.Background(), nil, AskRequest{Query: "producer", Ref: hash})
	if err != nil || result.IsError {
		t.Fatalf("ask: %v %+v", err, errText(result))
	}
	return &out
}

// With index.kind "memory", an answer says it was read through the index, and whether from the
// registries it held since an earlier call. What was published in between is in the next answer.
func TestIndex_AnAnswerSaysItCameFromTheMemoryIndex(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	out := pub.OutputHashes["data/out.csv"]

	if a := askProducer(t, out).Index; a != nil {
		t.Fatalf(`with index.kind unset ("none"), an answer names no index: %+v`, a)
	}

	raw := testrepo.DefaultConfig()
	raw.Index.Kind = "memory"
	r.WriteConfig(t, raw)
	askProducer(t, out) // the first open; opening may write to the registry
	askProducer(t, out)
	got := askProducer(t, out)
	if got.Index == nil || got.Index.Kind != "memory" || !got.Index.Kept {
		t.Fatalf("an unchanged registry is answered from what the index held: %+v", got.Index)
	}

	r.Write(t, "data/more.csv", "id,result\n2,168\n")
	result, second, err := Publish(context.Background(), nil, PublishRequest{
		Inputs: []string{"data/out.csv"}, Outputs: []string{"data/more.csv"}, Cmd: "double data/out.csv > data/more.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("publish: %v %+v", err, errText(result))
	}
	after := askProducer(t, second.OutputHashes["data/more.csv"])
	if after.Index == nil || after.Index.Kept {
		t.Fatalf("a publish changes the registry, so the next answer is read again: %+v", after.Index)
	}
	found := false
	for _, rec := range after.Records {
		found = found || rec.ID == second.FotonID
	}
	if !found {
		t.Fatalf("the record just published is missing from the next answer: %+v", after.Records)
	}
}
