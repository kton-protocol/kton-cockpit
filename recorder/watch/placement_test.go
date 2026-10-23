package watch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeCockpit answers `cockpit ask` like the programme example: the analysis repository federates
// a study, its project and the programme, and each holds the seed of its own scope.
type fakeCockpit struct {
	dirs     map[string]string            // dir → source name
	producer map[string]string            // sha256 → "tier foton"
	claims   map[string][]map[string]any  // foton → claims
	included map[string]bool              // verified claim ids
	seeds    map[string]map[string]string // source name → scope → parent ("" none)
	names    map[string]string            // scope → seed name
	asked    map[string]int               // query → times asked
}

func (f *fakeCockpit) ask(dir string, q map[string]string) ([]byte, error) {
	f.asked[q["query"]]++
	src := f.dirs[dir]
	switch q["query"] {
	case "producer":
		var recs []map[string]any
		if v, ok := f.producer[q["ref"]]; ok {
			var tier, id string
			for i, c := range v {
				if c == ' ' {
					tier, id = v[:i], v[i+1:]
				}
			}
			recs = append(recs, map[string]any{"id": id, "tier": tier, "verified": true})
		}
		return json.Marshal(map[string]any{"records": recs})
	case "about":
		var inc []string
		for _, c := range f.claims[q["ref"]] {
			if f.included[c["id"].(string)] {
				inc = append(inc, c["id"].(string))
			}
		}
		return json.Marshal(map[string]any{"claims": f.claims[q["ref"]], "included": inc})
	case "scope":
		parent, ok := f.seeds[src][q["ref"]]
		if !ok {
			return nil, errors.New("no readable scope seed")
		}
		return json.Marshal(map[string]any{"seal": map[string]any{"scope": q["ref"], "name": f.names[q["ref"]], "parent": parent}})
	}
	return nil, errors.New("unknown question")
}

func programme(t *testing.T) (string, *fakeCockpit) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "analysis")
	os.MkdirAll(repo, 0o755)
	src := func(n string) map[string]any {
		return map[string]any{"kind": "dir", "name": n, "path": filepath.Join(root, n)}
	}
	cfg, _ := json.Marshal(map[string]any{"federation": map[string]any{"sources": []any{
		src("program-respiratory"), src("project-theophylline"), src("study-thp-101"),
		map[string]any{"kind": "git", "name": "elsewhere", "url": "https://example.org/x.git"}}}})
	os.WriteFile(filepath.Join(repo, "cockpit.config.json"), cfg, 0o644)
	f := &fakeCockpit{
		dirs: map[string]string{repo: "self", filepath.Join(root, "program-respiratory"): "program-respiratory",
			filepath.Join(root, "project-theophylline"): "project-theophylline", filepath.Join(root, "study-thp-101"): "study-thp-101"},
		producer: map[string]string{"sha256:aaa": "study-thp-101 sha256:foton1"},
		claims: map[string][]map[string]any{"sha256:foton1": {
			{"id": "sha256:forged", "scope": "sha256:elsewhere"}, // not verified: never followed
			{"id": "sha256:c1", "scope": "sha256:study"},
		}},
		included: map[string]bool{"sha256:c1": true},
		seeds: map[string]map[string]string{
			"study-thp-101":        {"sha256:study": "sha256:project"},
			"project-theophylline": {"sha256:project": "sha256:programme"},
			"program-respiratory":  {"sha256:programme": ""},
		},
		names: map[string]string{"sha256:study": "study-thp-101", "sha256:project": "project-theophylline", "sha256:programme": "program-respiratory"},
		asked: map[string]int{},
	}
	return repo, f
}

func TestPlace_WalksTheScopeChainThroughTheFederatedSources(t *testing.T) {
	repo, f := programme(t)
	p := NewPlacer(f.ask).Place(repo, "data/thp101_pk.csv", "aaa")
	if !p.Placed || p.Foton != "sha256:foton1" || p.PublishedBy != "study-thp-101" {
		t.Fatalf("not placed as published: %+v", p)
	}
	want := []Level{{"study-thp-101", "sha256:study", "study-thp-101"}, {"project-theophylline", "sha256:project", "project-theophylline"},
		{"program-respiratory", "sha256:programme", "program-respiratory"}}
	if len(p.Levels) != len(want) {
		t.Fatalf("levels %+v, want %+v", p.Levels, want)
	}
	for i := range want {
		if p.Levels[i] != want[i] {
			t.Errorf("level %d: %+v, want %+v", i, p.Levels[i], want[i])
		}
	}
	if p.Note != "" {
		t.Errorf("note on a whole chain: %q", p.Note)
	}
}

func TestPlace_AnUnknownFileIsNotPlaced(t *testing.T) {
	repo, f := programme(t)
	p := NewPlacer(f.ask).Place(repo, "data/mine.csv", "bbb")
	if p.Placed || len(p.Levels) > 0 || p.Note != "not published in any federated, trusted repository" {
		t.Fatalf("an unknown file was placed: %+v", p)
	}
	if f.asked["about"]+f.asked["scope"] > 0 {
		t.Errorf("asked further about a file nobody published: %v", f.asked)
	}
}

func TestPlace_SaysWhereTheChainStops(t *testing.T) {
	repo, f := programme(t)
	delete(f.seeds, "program-respiratory") // the programme is not federated any more
	p := NewPlacer(f.ask).Place(repo, "data/thp101_pk.csv", "aaa")
	if len(p.Levels) != 2 || p.Note == "" {
		t.Fatalf("want two levels and a note where it stops, got %+v", p)
	}
}

func TestPlace_OnlyVerifiedClaimsNameTheScope(t *testing.T) {
	repo, f := programme(t)
	f.included = map[string]bool{}
	p := NewPlacer(f.ask).Place(repo, "data/thp101_pk.csv", "aaa")
	if !p.Placed || len(p.Levels) != 0 || p.Note == "" {
		t.Fatalf("an unverified claim placed the file: %+v", p)
	}
}

func TestPlace_SeedsAreLookedUpOnce(t *testing.T) {
	repo, f := programme(t)
	pl := NewPlacer(f.ask)
	pl.Place(repo, "a.csv", "aaa")
	n := f.asked["scope"]
	pl.Place(repo, "b.csv", "aaa")
	if f.asked["scope"] != n {
		t.Errorf("seeds asked again: %d, then %d", n, f.asked["scope"])
	}
}

func TestPlace_OutsideACockpitRepository(t *testing.T) {
	_, f := programme(t)
	p := NewPlacer(f.ask).Place(t.TempDir(), "x.csv", "aaa")
	if p.Placed || p.Note != "not a cockpit repository" || len(f.asked) > 0 {
		t.Fatalf("%+v, asked %v", p, f.asked)
	}
}

func TestPlaceSources_FromTheProjectAndOutsidePlacedFirst(t *testing.T) {
	repo, f := programme(t)
	st := &Story{Repo: repo}
	NewPlacer(f.ask).placeSources(st, repo, []*Input{
		{Path: "/opt/R/library/base/R/base", Class: ClassEnv, SHA256: "ccc"},
		{Path: "/data/shared/ref.csv", Class: ClassOutside, SHA256: "eee"},
		{Path: filepath.Join(repo, "data/old.csv"), Class: ClassProject, SHA256: "ddd", Status: "gone"},
		{Path: filepath.Join(repo, "data/thp101_pk.csv"), Class: ClassProject, SHA256: "aaa"},
	})
	// the dataset came last but leads: placed first
	if len(st.Context) != 2 || st.Context[0].Path != "data/thp101_pk.csv" || !st.Context[0].Placed ||
		st.Context[1].Path != "/data/shared/ref.csv" || st.Context[1].Placed {
		t.Fatalf("context %+v", st.Context)
	}
}
