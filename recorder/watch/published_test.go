package watch

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAddPublished_UpstreamFotonsBeforeRecordedSteps(t *testing.T) {
	recs := map[string]map[string]any{
		"sha256:prep": {"id": "sha256:prep", "cmd": "Rscript script/data-prep.R",
			"inputs":  []map[string]string{{"path": "data/raw.csv", "hash": "sha256:raw"}},
			"outputs": []map[string]string{{"path": "data/nm.csv", "hash": "sha256:nm"}}},
		"sha256:run1": {"id": "sha256:run1", "cmd": "nmfe75 run1.ctl run1.lst",
			"inputs":  []map[string]string{{"path": "data/nm.csv", "hash": "sha256:nm"}, {"path": "run1.ctl", "hash": "sha256:ctl"}},
			"outputs": []map[string]string{{"path": "run1.ext", "hash": "sha256:ext"}}},
		"sha256:gof": {"id": "sha256:gof", "cmd": "Rscript gof.R run1",
			"inputs":  []map[string]string{{"path": "run1.ext", "hash": "sha256:ext"}},
			"outputs": []map[string]string{{"path": "gof.png", "hash": "sha256:png"}}},
	}
	pl := NewPlacer(func(_ context.Context, _ string, q map[string]string) ([]byte, error) {
		switch q["query"] {
		case "lineage": // the target's producer first, as the cockpit answers
			return json.Marshal(map[string]any{"records": []map[string]string{{"id": "sha256:gof", "tier": "self"}, {"id": "sha256:run1", "tier": "self"}, {"id": "sha256:prep", "tier": "self"}}})
		case "record":
			return json.Marshal(map[string]any{"record": recs[q["ref"]]})
		}
		return nil, context.Canceled
	})
	st := &Story{Repo: "/r", Steps: []StoryStep{{Session: "terminal-1", Task: "t", When: 1000, Writes: []string{"gof.png"}, Hashes: []string{"png"}}},
		sources: []*Input{{Path: "/r/run1.ext", SHA256: "ext", Class: ClassProject}}}
	pl.addPublished(context.Background(), st)
	if len(st.Steps) != 3 {
		t.Fatalf("want prep, run1 (published) and the recorded gof; got %d steps: %+v", len(st.Steps), st.Steps)
	}
	if st.Steps[0].Published == nil || st.Steps[0].Published.Cmd != "Rscript script/data-prep.R" || st.Steps[1].Published.Foton != "sha256:run1" {
		t.Fatalf("upstream first: %+v", st.Steps)
	}
	if st.Steps[2].Session != "terminal-1" || st.Steps[1].Session != "kton" || st.Steps[1].Hashes[0] != "ext" || !(st.Steps[0].When < st.Steps[1].When && st.Steps[1].When < 1000) {
		t.Fatalf("the recorded step stays, the gof foton recorded here is not repeated: %+v", st.Steps)
	}
}
