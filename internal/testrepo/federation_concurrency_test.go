package testrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// Many readers of one federated repository at once — an editor, a watcher and a script, each its
// own process — all answer, while the source changes under them. They used to rebuild one union
// directory in place and tear it down under each other ("unlinkat …/union: directory not empty").
func TestFederation_ConcurrentReadersAllAnswerWhileTheSourceChanges(t *testing.T) {
	ctx := context.Background()
	theirs := New(t)
	tc := theirs.Config(t)
	author := func(i int) string {
		in, out := fmt.Sprintf("data/in%d.csv", i), fmt.Sprintf("data/out%d.csv", i)
		theirs.Write(t, in, fmt.Sprintf("id,value\n%d,42\n", i))
		theirs.Write(t, out, fmt.Sprintf("id,result\n%d,84\n", i))
		a, err := binaries.New(tc).Author(ctx, binaries.AuthorInput{Inputs: []string{in}, Outputs: []string{out},
			Cmd: "double " + in + " > " + out, SignKey: tc.PlanktonKey})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	first := author(0)

	ours := New(t)
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	raw := DefaultConfig()
	raw.Federation.Sources = []source.Spec{spec}
	ours.WriteConfig(t, raw)

	const readers = 16
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each its own config and Runner, as each process would have.
			if _, err := binaries.New(ours.Config(t)).FotonByID(ctx, first); err != nil {
				errs <- err
			}
		}()
	}
	for i := 1; i <= 3; i++ { // the source changes while they read: each change is a new union
		author(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a concurrent reader failed: %v", err)
	}
}
