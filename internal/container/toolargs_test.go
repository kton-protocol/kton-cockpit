package container

import (
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The engine runs bounded in memory and processes, without network (kton-review L1 on #235): a query
// run may read bytes someone else's record lists.
func TestToolArgs_TheEngineIsBounded(t *testing.T) {
	cfg := &config.Config{}
	got := strings.Join(toolArgs(cfg, "/d", "img"), " ")
	for _, want := range []string{"--network none", "--memory 4g", "--memory-swap 4g", "--pids-limit 256", "--cap-drop=ALL"} {
		if !strings.Contains(got, want) {
			t.Errorf("the engine's arguments say %q: %s", want, got)
		}
	}
	cfg.Raw.Query.Memory = "1g"
	if got := strings.Join(toolArgs(cfg, "/d", "img"), " "); !strings.Contains(got, "--memory 1g --memory-swap 1g") {
		t.Errorf("query.memory bounds it: %s", got)
	}
}
