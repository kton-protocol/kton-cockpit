//go:build docker

package oxigraph_test

import (
	"context"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// While an ask builds the view, the index says so for that repository; once the ask has its answer, no
// build is listed (userinterface, T12).
func TestOxigraph_ABuildIsListedWhileItRuns(t *testing.T) {
	ctx := context.Background()
	idx := t.TempDir()
	t.Setenv("COCKPIT_INDEX_DIR", idx)
	r := testrepo.New(t)
	cfg := indexed(t, r)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	author(t, r, cfg, "one")
	var during []oxigraph.Building
	oxigraph.SetDuringBuild(func() { during = oxigraph.BuildsRunning(idx, cfg.RepoRoot) })
	t.Cleanup(func() { oxigraph.SetDuringBuild(nil) })
	if _, err := oxigraph.Free(ctx, d, cfg, keys(t, cfg), "ASK { }", oxigraph.DatasetClaims, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(during) != 1 || during[0].Repo != cfg.RepoRoot || during[0].Phase == "" {
		t.Errorf("the build is listed for its repository while it runs: %+v", during)
	}
	if after := oxigraph.BuildsRunning(idx, cfg.RepoRoot); len(after) != 0 {
		t.Errorf("no build listed once the answer is there: %+v", after)
	}
}
