package testrepo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
)

// Asking about a record a federation source holds asks for its locators too, and those are read
// from the same union. They were read from the own registry only, so `ask reproductions` over a
// peer's result failed with "no foton … in this registry" for every federated record.
func TestFederation_LocatorsOfAFederatedRecord(t *testing.T) {
	theirs := New(t)
	theirs.Write(t, "data/in.csv", "id,value\n1,42\n")
	theirs.Write(t, "data/out.csv", "id,result\n1,84\n")
	tc := theirs.Config(t)
	a, err := binaries.New(tc).Author(context.Background(), binaries.AuthorInput{Inputs: []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"}, Cmd: "double data/in.csv > data/out.csv", SignKey: tc.PlanktonKey,
		Located: []string{"data/in.csv=https://example.test/theirs/data/in.csv"}})
	if err != nil {
		t.Fatal(err)
	}

	oc := New(t).Config(t)
	run := binaries.New(oc)
	// The negative control: not a source, not found.
	if _, err := verify.Locators(context.Background(), run, oc, a.ID); err == nil {
		t.Fatal("a record no source holds was found")
	}
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	oc.Raw.Federation.Sources = []source.Spec{spec}
	// Theirs are signed by a key of theirs, so only once ours trusts it are they passed on.
	if locs, err := verify.Locators(context.Background(), run, oc, a.ID); err != nil || len(locs) != 0 {
		t.Fatalf("locators signed by a key no tier names: %v %+v", err, locs)
	}
	oc.Raw.Trust.Tiers["peer"] = []string{filepath.Join(tc.RepoRoot, "registry", "keys", SessionID+".pub")}
	locs, err := verify.Locators(context.Background(), run, oc, a.ID)
	if err != nil || len(locs) != 1 || locs[0].Path != "data/in.csv" || locs[0].Tier != "peer" ||
		len(locs[0].URI) != 1 || locs[0].URI[0] != "https://example.test/theirs/data/in.csv" {
		t.Fatalf("the locators of a federated record, signed by a trusted peer: %v %+v", err, locs)
	}
}
