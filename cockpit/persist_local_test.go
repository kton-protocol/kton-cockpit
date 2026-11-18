package cockpit

import (
	"context"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// Nothing under .kton/local/ is persisted by any backend, and neither is .kton/ as a whole, which
// would include it: the refusal is the cockpit's, not .gitignore's (ADR-016, kton-review L1).
func TestPersist_NeverTakesKtonLocal(t *testing.T) {
	r := testrepo.New(t)
	cfg := r.Config(t)
	before := r.HeadSHA(t)
	for _, p := range []string{".kton/local", ".kton/local/identity.json", ".kton/LOCAL/calls", ".kton", ".", ".kton/./local"} {
		if _, err := persist(context.Background(), cfg, []string{p}, "leak"); err == nil || !strings.Contains(err.Error(), "never persisted") {
			t.Errorf("%s: want refused, got %v", p, err)
		}
	}
	if r.HeadSHA(t) != before {
		t.Fatal("a refused persist committed")
	}
	// The negative control: the registries under .kton/ are persisted as before.
	if _, err := persist(context.Background(), cfg, []string{".kton/nekton"}, "fine"); err != nil {
		t.Fatalf(".kton/nekton: %v", err)
	}
}
