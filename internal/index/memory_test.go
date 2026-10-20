package index_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// pair is a repository that federates another one, with the configuration's index set to kind.
func pair(t *testing.T, kind string) (own *testrepo.Repo, ours *config.Config, theirs *testrepo.Repo, tc *config.Config) {
	theirs = testrepo.New(t)
	tc = theirs.Config(t)
	own = testrepo.New(t)
	ours = own.Config(t)
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	ours.Raw.Federation.Sources = []source.Spec{spec}
	ours.Raw.Index.Kind = kind
	return own, ours, theirs, tc
}

func author(t *testing.T, r *testrepo.Repo, cfg *config.Config, name string) string {
	t.Helper()
	r.Write(t, "data/"+name+".in", name+"\n")
	r.Write(t, "data/"+name+".out", name+" done\n")
	a, err := binaries.New(cfg).Author(context.Background(), binaries.AuthorInput{
		Inputs: []string{"data/" + name + ".in"}, Outputs: []string{"data/" + name + ".out"},
		Cmd: "do " + name, SignKey: cfg.PlanktonKey})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func ids(t *testing.T, r index.Reader) []string {
	t.Helper()
	recs, err := r.Records(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, rec := range recs {
		out = append(out, rec.ID())
	}
	sort.Strings(out)
	return out
}

// The memory index keeps the opened union from one call to the next while nothing it was opened
// from changes, and says so in what it answers.
func TestMemory_KeepsTheUnionAcrossCallsWhileNothingChanged(t *testing.T) {
	ctx := context.Background()
	_, ours, theirs, tc := pair(t, "memory")
	id := author(t, theirs, tc, "a")

	first := index.Open(ours)
	if _, err := first.FotonByID(ctx, id); err != nil {
		t.Fatal(err)
	}
	if a := index.From(first); a == nil || a.Kind != "memory" || a.Kept {
		t.Fatalf("the first call opens the union: %+v", a)
	}
	// Opening a registry for the first time can write to it (the kernel persists its numbering),
	// which the next call sees as a change. From then on nothing changes.
	settle := index.Open(ours)
	if _, err := settle.FotonByID(ctx, id); err != nil {
		t.Fatal(err)
	}
	a := index.From(settle)
	second := index.Open(ours)
	if _, err := second.FotonByID(ctx, id); err != nil {
		t.Fatal(err)
	}
	if b := index.From(second); b == nil || !b.Kept || b.Opened != a.Opened {
		t.Fatalf("a later call with nothing changed keeps it: %+v, first %+v", b, a)
	}
	if index.From(index.Open(withKind(ours, "none"))) != nil {
		t.Fatal(`"none" holds nothing and says nothing about an index`)
	}
}

// Whatever changes the registries — a record a source gained, a write in this repository, a
// registry that went away — is in the next answer, exactly as "none" reads it.
func TestMemory_AnswersAsNoneAfterAnyChange(t *testing.T) {
	own, ours, theirs, tc := pair(t, "memory")
	author(t, theirs, tc, "a")
	same := func(when string) {
		t.Helper()
		m, n := index.Open(ours), index.Open(withKind(ours, "none"))
		got, want := ids(t, m), ids(t, n)
		if len(got) != len(want) {
			t.Fatalf("%s: memory read %v, none %v", when, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: memory read %v, none %v", when, got, want)
			}
		}
	}
	same("at first")
	same("unchanged")

	author(t, theirs, tc, "b")
	same("after the source gained a record")

	author(t, own, ours, "c")
	same("after a write in this repository")

	if err := os.RemoveAll(tc.PlanktonDir); err != nil {
		t.Fatal(err)
	}
	same("after the source's registry went away")
}

func TestMemory_AnUnknownKindIsRefusedAtLoad(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Index.Kind = "elsewhere"
	r.WriteConfig(t, raw)
	if _, err := config.Load(context.Background(), r.Root); err == nil {
		t.Fatal("an index kind this build does not have must be refused")
	}
}

func withKind(cfg *config.Config, kind string) *config.Config {
	c := *cfg
	c.Raw.Index.Kind = kind
	return &c
}
