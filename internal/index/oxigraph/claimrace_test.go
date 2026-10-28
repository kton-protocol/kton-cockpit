//go:build docker

package oxigraph_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A reader of the query projection never sees a view without its claims while other writes rebuild
// it: the view, its claims and its mark change in one update (ADR-013 review V1, whose probe this is;
// before, 3–7 answers in about 130 lacked a claim the registry held throughout).
func TestOxigraph_AReaderNeverSeesAViewWithoutItsClaims(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	alice := testrepo.New(t)
	ac := indexed(t, alice)
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	scope := alice.SeedScope(t, "s")
	c1 := alice.ChainClaimDirectly(t, "https://ex.org/step1", scope, scope, "step1")
	want := "https://kton.dev/o/" + strings.TrimPrefix(c1, "sha256:")
	const q = `SELECT ?c WHERE { ?c a <https://kton.dev/v/Claim> }`
	has := func() (bool, int, error) {
		a, err := oxigraph.Free(ctx, d, ac, keys(t, ac), q, oxigraph.DatasetClaims, time.Minute)
		if err != nil {
			return false, 0, err
		}
		for _, r := range a.Results.Rows {
			if r["c"] == want {
				return true, len(a.Results.Rows), nil
			}
		}
		return false, len(a.Results.Rows), nil
	}
	if ok, n, err := has(); err != nil || !ok {
		t.Fatalf("claim not in the view to begin with: ok=%v rows=%d err=%v", ok, n, err)
	}

	var misses, reads atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ok, _, err := has()
				if err != nil {
					t.Error(err)
					return
				}
				reads.Add(1)
				if !ok {
					misses.Add(1)
				}
			}
		}()
	}
	for i := 0; i < 30; i++ {
		author(t, alice, ac, fmt.Sprintf("w%d", i))
		if _, _, err := has(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	t.Logf("30 writes, %d concurrent answers, %d without the claim", reads.Load(), misses.Load())
	if misses.Load() > 0 {
		t.Errorf("%d answers of the query projection lacked a claim the registry held throughout", misses.Load())
	}
}
