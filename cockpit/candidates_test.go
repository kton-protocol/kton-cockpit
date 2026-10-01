package cockpit

import (
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg"
)

// propose numbers, extract takes the numbers: files first (code excluded), then command-line
// places; a number used for the wrong kind is refused by name rather than taken as something else.
func TestCandidates_NumbersArePicksAndTheKindIsChecked(t *testing.T) {
	p := &ktonpkg.Proposal{
		Files: []ktonpkg.FileCandidate{
			{Step: "a", Slot: "run.R", CommandFile: true},
			{Step: "a", Slot: "data.csv"},
		},
		Params: []ktonpkg.ParamCandidate{{Step: "b", Position: 2}},
	}
	c := Candidates(p)
	if len(c) != 2 || c[0].Kind != "file" || c[0].Slot != "data.csv" || c[1].Kind != "param" || c[1].N != 2 {
		t.Fatalf("candidates %+v", c)
	}
	var ch ktonpkg.Choice
	if err := resolvePicks(p, []Pick{{Name: "dataset", Kind: "hole", Number: 1}, {Name: "n", Kind: "param", Number: 2}}, &ch); err != nil {
		t.Fatal(err)
	}
	if ch.Holes[0].Slot != "data.csv" || ch.Params[0].Position != 2 {
		t.Fatalf("choice %+v", ch)
	}
	err := resolvePicks(p, []Pick{{Name: "x", Kind: "hole", Number: 2}}, &ktonpkg.Choice{})
	if err == nil || !strings.Contains(err.Error(), "--param") {
		t.Fatalf("a param number used as a hole: %v", err)
	}
	if err := resolvePicks(p, []Pick{{Name: "x", Kind: "hole", Number: 3}}, &ktonpkg.Choice{}); err == nil {
		t.Fatal("a number past the candidates was taken")
	}
}
