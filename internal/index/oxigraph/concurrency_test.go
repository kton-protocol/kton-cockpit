//go:build docker && unreleased

package oxigraph_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A reader does not wait for the lock. While other queries read the registry again from 0 (records
// arriving unnumbered, a newly trusted key), no answer lacks a record the registry held throughout.
// (Found by kton-review on PR #8.)
func TestOxigraph_AReaderNeverSeesARegistryHalfRead(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	alice := testrepo.New(t)
	ac := indexed(t, alice)
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	first := author(t, alice, ac, "a")

	// carol's records, planted one by one into alice's objects as a merge would bring them:
	// objects move, maxSeq does not, so each query after a plant reads alice's registry from 0.
	carol := testrepo.New(t)
	cc := carol.Config(t)
	for _, n := range []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10", "c11", "c12"} {
		author(t, carol, cc, n)
	}
	src := filepath.Join(carol.Root, "registry", "plankton", "objects")
	dst := filepath.Join(alice.Root, "registry", "plankton", "objects")
	var files []string
	filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && filepath.Base(p) != ".format" {
			rel, _ := filepath.Rel(src, p)
			files = append(files, rel)
		}
		return nil
	})
	query(t, d, ac, activities)

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
				a, err := oxigraph.Query(ctx, d, ac, keys(t, ac), activities)
				if err != nil {
					t.Error(err)
					return
				}
				reads.Add(1)
				if !holds(a, "a", first) {
					misses.Add(1)
				}
			}
		}()
	}
	for _, rel := range files {
		b, _ := os.ReadFile(filepath.Join(src, rel))
		os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755)
		os.WriteFile(filepath.Join(dst, rel), b, 0o644)
		if _, err := oxigraph.Query(ctx, d, ac, keys(t, ac), activities); err != nil {
			t.Fatal(err)
		}
	}
	// A query trusting one more key re-reads from 0 while the position stays the same, so readers
	// under the old keys find the registry current and do not wait for the lock.
	for i := 0; i < 40; i++ {
		k, _, _ := ed25519.GenerateKey(rand.Reader)
		if _, err := oxigraph.Query(ctx, d, ac, append(keys(t, ac), k), activities); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if misses.Load() > 0 {
		t.Errorf("%d of %d answers lacked a record the registry held throughout", misses.Load(), reads.Load())
	}
}
