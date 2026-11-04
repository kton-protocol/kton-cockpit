//go:build docker

package oxigraph_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The daemon runs with --restart unless-stopped, so the engine brings it back after a crash or a
// reboot; the next cockpit finds it at the port the engine chose anew and uses it, on its store,
// rather than starting another.
func TestOxigraph_ADaemonTheEngineRestartedIsFoundAgain(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	r := testrepo.New(t)
	cfg := indexed(t, r)
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	f := author(t, r, cfg, "one")
	const attributed = `SELECT ?a WHERE { ?a <http://www.w3.org/ns/prov#wasAttributedTo> ?k }`
	if a := query(t, d, cfg, attributed); !holds(a, "a", f) {
		t.Fatalf("want f read in: %+v", a)
	}
	_, l, _ := oxigraph.Running(ctx)
	if l == nil {
		t.Fatal("want a lock")
	}
	inspect := func(format string) string {
		t.Helper()
		out, err := exec.Command("docker", "inspect", "--format", format, l.Container).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	if p := inspect("{{.HostConfig.RestartPolicy.Name}}"); p != "unless-stopped" {
		t.Fatalf("want restart policy unless-stopped, got %q", p)
	}
	id := inspect("{{.Id}}")

	// What the engine does after a crash: the same container, started again, its port chosen anew.
	if out, err := exec.Command("docker", "restart", l.Container).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	d2, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := inspect("{{.Id}}"); got != id {
		t.Fatal("want the restarted container used, not a new one")
	}
	if _, l2, _ := oxigraph.Running(ctx); l2 == nil || l2.URL != d2.URL {
		t.Fatalf("want the lock to name where it answers now (%s): %+v", d2.URL, l2)
	}
	if a := query(t, d2, cfg, attributed); !holds(a, "a", f) || !a.Kept {
		t.Fatalf("want f without reading in again: %+v", a)
	}
}
