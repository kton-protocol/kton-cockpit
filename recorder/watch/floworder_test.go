package watch

import "testing"

// the clock stepped back: the report is stamped before the table it read; the table goes first
func TestFlowOrder_ProducerBeforeReader(t *testing.T) {
	steps := []StoryStep{
		{Task: "eda", When: 1, Writes: []string{"e.png"}, Hashes: []string{"e"}, Reads: []string{"d.csv"}, InHashes: []string{"d"}},
		{Task: "report", When: 2, Writes: []string{"r.html"}, Hashes: []string{"r"}, Reads: []string{"t.csv", "e.png"}, InHashes: []string{"t", "e"}},
		{Task: "table", When: 3, Writes: []string{"t.csv"}, Hashes: []string{"t"}, Reads: []string{"d.csv"}, InHashes: []string{"d"}},
		{Task: "data", When: 4, Writes: []string{"d.csv"}, Hashes: []string{"d"}},
		// the same bytes at another path (a copy of a model writes the same table): no producer of t.csv
		{Task: "copy", When: 5, Writes: []string{"t8.csv"}, Hashes: []string{"t"}},
	}
	got := ""
	for _, s := range flowOrder(steps) {
		got += s.Task + " "
	}
	if got != "data eda table report copy " {
		t.Fatalf("order %q", got)
	}
	// a cycle does not hang: by time
	cyc := []StoryStep{{Task: "a", Writes: []string{"x"}, Hashes: []string{"x"}, Reads: []string{"y"}, InHashes: []string{"y"}}, {Task: "b", Writes: []string{"y"}, Hashes: []string{"y"}, Reads: []string{"x"}, InHashes: []string{"x"}}}
	if out := flowOrder(cyc); len(out) != 2 || out[0].Task != "a" {
		t.Fatalf("cycle %v", out)
	}
}
