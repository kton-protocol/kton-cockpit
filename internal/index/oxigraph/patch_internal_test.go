package oxigraph

import "testing"

// A graph's state after a patch is the hash of its state before and of what the patch deleted and inserted:
// a triple in both sets stays where it is and counts for nothing, a patch that moves nothing leaves the
// state as it was, and the same edit made twice gives the same state (ADR-022 Q2).
func TestPatch_AGraphsStateChangesWithItsContentAndOtherwiseNot(t *testing.T) {
	q := func(o string) Quad { return triple("urn:s", "urn:p", lit(o)) }
	e := edits{}
	e.remove("g", q("a"))
	e.insert("g", q("a")) // deleted and inserted: stays
	e.insert("g", q("b"))
	e.remove("h", q("c"))
	e.insert("h", q("c")) // nothing else in h: no edit at all
	e.settle()
	if _, ok := e["h"]; ok {
		t.Errorf("a graph whose only edit cancels is still edited: %+v", e["h"])
	}
	if len(e["g"].del) != 0 || len(e["g"].ins) != 1 {
		t.Fatalf("g: %d deleted, %d inserted, want 0 and 1", len(e["g"].del), len(e["g"].ins))
	}
	s1 := nextState("old", e["g"])
	if s1 == "old" || s1 != nextState("old", e["g"]) {
		t.Errorf("the state is not a function of the edit that moves it: %s", s1)
	}
	other := edits{}
	other.insert("g", q("c"))
	if nextState("old", other["g"]) == s1 || nextState("newer", e["g"]) == s1 {
		t.Errorf("another edit or another state before gives the same state")
	}
}
