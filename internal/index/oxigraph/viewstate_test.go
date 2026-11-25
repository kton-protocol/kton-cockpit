package oxigraph

import (
	"reflect"
	"testing"
)

// Two states of one view name the graphs whose hash changed or that came, and those that went; states
// that cannot be compared graph by graph say all (SPEC §12.5).
func TestViewState_DiffNamesChangedAndRemovedGraphs(t *testing.T) {
	old := ViewState{Slot: "s", Key: "k1", Graphs: map[string]string{"a": "1", "b": "2", "gone": "3"}}
	now := ViewState{Slot: "s", Key: "k2", Graphs: map[string]string{"a": "1", "b": "9", "new": "4"}}
	c := now.Diff(old)
	if c.All || !reflect.DeepEqual(c.Graphs, []string{"b", "new"}) || !reflect.DeepEqual(c.Removed, []string{"gone"}) {
		t.Errorf("changed b, came new, went gone: %+v", c)
	}
	if c := now.Diff(now); c.All || len(c.Graphs) != 0 || len(c.Removed) != 0 {
		t.Errorf("nothing changed: %+v", c)
	}
	for name, o := range map[string]ViewState{
		"no state":     {},
		"another view": {Slot: "t", Graphs: now.Graphs},
		"no hashes":    {Slot: "s", Key: "k0"},
	} {
		if c := now.Diff(o); !c.All {
			t.Errorf("%s: want all, got %+v", name, c)
		}
	}
	if c := (ViewState{Slot: "s"}).Diff(old); !c.All {
		t.Errorf("a view without hashes now: want all, got %+v", c)
	}
}

func TestViewState_ScopeGraphNamesTheSeedsGraph(t *testing.T) {
	if got := ScopeGraph("s", oNS+"ab"); got != scopeGraphIRI("s", "ab") {
		t.Errorf("ScopeGraph = %s", got)
	}
}
