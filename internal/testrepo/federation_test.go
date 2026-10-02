package testrepo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// A record another registry holds is read together with this repository's own once that registry
// is named as a federation source — and only then. Nothing is copied into the own registry.
func TestFederation_ReadsTheUnionOfOwnAndSources(t *testing.T) {
	theirs := New(t)
	theirs.Write(t, "data/in.csv", "id,value\n1,42\n")
	theirs.Write(t, "data/out.csv", "id,result\n1,84\n")
	tc := theirs.Config(t)
	a, err := binaries.New(tc).Author(context.Background(), binaries.AuthorInput{Inputs: []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"}, Cmd: "double data/in.csv > data/out.csv", SignKey: tc.PlanktonKey})
	if err != nil {
		t.Fatal(err)
	}

	ours := New(t)
	oc := ours.Config(t)
	run := binaries.New(oc)
	if _, err := run.FotonByID(context.Background(), a.ID); err == nil {
		t.Fatal("without a source, a foreign record must not be found")
	}

	spec := source.Spec{}
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	if err := source.Validate([]source.Spec{spec}); err != nil {
		t.Fatal(err)
	}
	oc.Raw.Federation.Sources = []source.Spec{spec}
	got, err := run.FotonByID(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("with the source named, its record is read: %v", err)
	}
	_ = got
	recs, err := run.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recs {
		if r.FotonID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the union served by show must hold the source's record")
	}
	// The own registry is untouched.
	oc.Raw.Federation.Sources = nil
	if _, err := run.FotonByID(context.Background(), a.ID); err == nil {
		t.Fatal("federating must not copy foreign records into the own registry")
	}
}

func TestFederation_UnknownKindIsRefusedAtLoad(t *testing.T) {
	var spec source.Spec
	_ = json.Unmarshal([]byte(`{"name":"x","kind":"carrier-pigeon"}`), &spec)
	err := source.Validate([]source.Spec{spec})
	if err == nil || !strings.Contains(err.Error(), "not available in this build") {
		t.Fatalf("got %v", err)
	}
}
