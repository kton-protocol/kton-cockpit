package watch

import "testing"

func TestKtonGraph_StepsSourcesAndBuildsOn(t *testing.T) {
	doc := ktonGraphOf(workday(), "/p", func(p, s string) string { return "/blob/" + s })
	g := doc.Graph
	types := map[string]int{}
	for _, n := range g.Nodes {
		types[n.Type]++
	}
	if types["foton"] != 8 || types["source"] != 2 {
		t.Fatalf("types %v", types)
	}
	out := StepNodeID("s2", "4") // write.csv(m, "fuer_sie.csv")
	var found bool
	for _, e := range g.Edges {
		if e.From == out && e.To == StepNodeID("s2", "2") && e.Kind == "builds-on" {
			found = true // through the object m
		}
	}
	if !found {
		t.Fatalf("the writing step must build on the merge through m: %+v", g.Edges)
	}
	if g.Summary.Totals.Fotons != 8 || g.Summary.Totals.Unverified != 8 || len(g.Locators) == 0 {
		t.Fatalf("summary %+v locators %d", g.Summary.Totals, len(g.Locators))
	}
}
