package watch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCockpit answers `cockpit ask` like the programme example: the analysis repository federates
// a study, its project and the programme, and each holds the seed of its own scope.
type fakeCockpit struct {
	dirs     map[string]string            // dir → source name
	producer map[string]string            // sha256 → "tier foton"
	claims   map[string][]map[string]any  // foton → claims
	included map[string]bool              // verified claim ids
	tier     map[string]string            // claim id → tier that verified it
	hang     bool                         // answer nothing until asked to stop
	union    bool                         // ask scope over the federation, verified here (SPEC §9.6)
	distrust map[string]bool              // scope → its seed's key is in no tier here
	seeds    map[string]map[string]string // source name → scope → parent ("" none)
	names    map[string]string            // scope → seed name
	asked    map[string]int               // query → times asked
}

func (f *fakeCockpit) ask(ctx context.Context, dir string, q map[string]string) ([]byte, error) {
	f.asked[q["query"]]++
	if f.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
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
		var recs []map[string]any
		for _, c := range f.claims[q["ref"]] {
			if id := c["id"].(string); f.included[id] {
				inc = append(inc, id)
				recs = append(recs, map[string]any{"id": id, "tier": f.tier[id], "verified": true})
			}
		}
		return json.Marshal(map[string]any{"claims": f.claims[q["ref"]], "included": inc, "records": recs})
	case "scope":
		if f.union {
			return f.unionScope(q["ref"])
		}
		parent, ok := f.seeds[src][q["ref"]]
		if !ok {
			return nil, errors.New("no readable scope seed")
		}
		return json.Marshal(map[string]any{"seal": map[string]any{"scope": q["ref"], "name": f.names[q["ref"]], "parent": parent}})
	}
	return nil, errors.New("unknown question")
}

func (f *fakeCockpit) unionScope(scope string) ([]byte, error) {
	find := func(sc string) (string, string, bool) {
		for src, seeds := range f.seeds {
			if parent, ok := seeds[sc]; ok {
				return src, parent, true
			}
		}
		return "", "", false
	}
	src, parent, ok := find(scope)
	if !ok {
		return nil, errors.New("no seed for it in this repository or its federation sources")
	}
	if f.distrust[scope] {
		return json.Marshal(map[string]any{"seal": map[string]any{"scope": scope, "seedTier": ""}})
	}
	var ctx []map[string]any
	for sc := parent; sc != ""; {
		s, p, ok := find(sc)
		switch {
		case !ok:
			ctx = append(ctx, map[string]any{"scope": sc, "missing": true})
		case f.distrust[sc]:
			ctx = append(ctx, map[string]any{"scope": sc, "tier": ""})
		default:
			ctx = append(ctx, map[string]any{"scope": sc, "name": f.names[sc], "tier": s})
			sc = p
			continue
		}
		break
	}
	return json.Marshal(map[string]any{"seal": map[string]any{"scope": scope, "name": f.names[scope], "parent": parent, "seedTier": src, "context": ctx}})
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
		producer: map[string]string{"sha256:aaa": "study-thp-101 sha256:foton1", "sha256:aa2": "study-thp-101 sha256:foton1"},
		claims: map[string][]map[string]any{"sha256:foton1": {
			{"id": "sha256:forged", "scope": "sha256:elsewhere"}, // not verified: never followed
			{"id": "sha256:c1", "scope": "sha256:study"},
		}},
		included: map[string]bool{"sha256:c1": true},
		tier:     map[string]string{"sha256:c1": "study-thp-101"},
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
	p := NewPlacer(f.ask).Place(context.Background(), repo, "data/thp101_pk.csv", "aaa")
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
	p := NewPlacer(f.ask).Place(context.Background(), repo, "data/mine.csv", "bbb")
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
	p := NewPlacer(f.ask).Place(context.Background(), repo, "data/thp101_pk.csv", "aaa")
	if len(p.Levels) != 2 || p.Note == "" {
		t.Fatalf("want two levels and a note where it stops, got %+v", p)
	}
}

func TestPlace_OnlyVerifiedClaimsNameTheScope(t *testing.T) {
	repo, f := programme(t)
	f.included = map[string]bool{}
	p := NewPlacer(f.ask).Place(context.Background(), repo, "data/thp101_pk.csv", "aaa")
	if !p.Placed || len(p.Levels) != 0 || p.Note == "" {
		t.Fatalf("an unverified claim placed the file: %+v", p)
	}
}

func TestPlace_SeedsAreLookedUpOnce(t *testing.T) {
	repo, f := programme(t)
	pl := NewPlacer(f.ask)
	pl.Place(context.Background(), repo, "a.csv", "aaa")
	n := f.asked["scope"]
	pl.Place(context.Background(), repo, "b.csv", "aa2") // other bytes, same foton: same seeds
	if f.asked["scope"] != n {
		t.Errorf("seeds asked again: %d, then %d", n, f.asked["scope"])
	}
}

// review of #23, C3: a placement is kept briefly, then asked again (it may be published meanwhile)
func TestPlace_APlacementIsKeptBriefly(t *testing.T) {
	repo, f := programme(t)
	now := time.Unix(0, 0)
	pl := NewPlacer(f.ask)
	pl.Now = func() time.Time { return now }
	pl.Place(context.Background(), repo, "a.csv", "bbb")
	pl.Place(context.Background(), repo, "a.csv", "bbb")
	if f.asked["producer"] != 1 {
		t.Fatalf("producer asked %d times within the minute", f.asked["producer"])
	}
	f.producer["sha256:bbb"] = "study-thp-101 sha256:foton1" // published since
	now = now.Add(2 * time.Minute)
	if p := pl.Place(context.Background(), repo, "a.csv", "bbb"); !p.Placed || f.asked["producer"] != 2 {
		t.Fatalf("not asked again after the minute: %+v, %d", p, f.asked["producer"])
	}
}

// review of #23, C2: a seed not found is looked for again once a federation pull may have brought it
func TestPlace_AMissingSeedIsLookedForAgain(t *testing.T) {
	repo, f := programme(t)
	programmeSeed := f.seeds["program-respiratory"]
	delete(f.seeds, "program-respiratory")
	now := time.Unix(0, 0)
	pl := NewPlacer(f.ask)
	pl.Now = func() time.Time { return now }
	if p := pl.Place(context.Background(), repo, "a.csv", "aaa"); len(p.Levels) != 2 {
		t.Fatalf("levels %+v", p.Levels)
	}
	f.seeds["program-respiratory"] = programmeSeed // pulled
	now = now.Add(2 * time.Minute)
	if p := pl.Place(context.Background(), repo, "a.csv", "aaa"); len(p.Levels) != 3 {
		t.Fatalf("the pulled seed was not found: %+v", p)
	}
}

// review of #23, C1: a cockpit that does not answer is cut off within the story's budget
func TestPlaceSources_AHangingCockpitIsCutOff(t *testing.T) {
	repo, f := programme(t)
	f.hang = true
	pl := NewPlacer(f.ask)
	pl.Budget = 50 * time.Millisecond
	st := &Story{Repo: repo}
	start := time.Now()
	pl.placeSources(st, repo, []*Input{
		{Path: filepath.Join(repo, "a.csv"), Class: ClassProject, SHA256: "aaa"},
		{Path: filepath.Join(repo, "b.csv"), Class: ClassProject, SHA256: "aa2"},
	})
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("waited %v for a hanging cockpit", d)
	}
	if len(st.Context) != 1 || st.Context[0].Note != "the cockpit did not answer in time" {
		t.Fatalf("context %+v", st.Context)
	}
	f.hang = false
	if p := pl.Place(context.Background(), repo, "a.csv", "aaa"); !p.Placed {
		t.Fatalf("an answer cut short was kept: %+v", p)
	}
}

// review of #23: the publisher's own claim decides the chain, not whichever verified claim comes first
func TestPlace_ThePublishersClaimComesFirst(t *testing.T) {
	repo, f := programme(t)
	f.claims["sha256:foton1"] = []map[string]any{
		{"id": "sha256:other", "scope": "sha256:programme"}, // another trusted repo files it under the programme
		{"id": "sha256:c1", "scope": "sha256:study"},
	}
	f.included["sha256:other"] = true
	f.tier["sha256:other"] = "program-respiratory"
	p := NewPlacer(f.ask).Place(context.Background(), repo, "a.csv", "aaa")
	if len(p.Levels) != 3 || p.Levels[0].Name != "study-thp-101" {
		t.Fatalf("levels %+v", p.Levels)
	}
}

func TestPlace_OutsideACockpitRepository(t *testing.T) {
	_, f := programme(t)
	p := NewPlacer(f.ask).Place(context.Background(), t.TempDir(), "x.csv", "aaa")
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

// Concurrent `cockpit ask`s in one repository with federation sources tear down each other's
// union cache: CockpitAsk asks one at a time per directory. The fake cockpit fails on overlap.
func TestCockpitAsk_OneAtATimePerRepository(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cockpit")
	// an overlap is written down, not only failed: the retry would hide a failure
	os.WriteFile(exe, []byte("#!/bin/sh\nmkdir \"$PWD/busy\" 2>/dev/null || { touch \"$PWD/overlapped\"; exit 3; }\nsleep 0.05\nrmdir \"$PWD/busy\"\necho '{}'\n"), 0o755)
	t.Setenv("KTON_COCKPIT", exe)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := CockpitAsk(context.Background(), dir, map[string]string{"query": "producer", "ref": "sha256:x"})
			errs <- err
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("an ask failed: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "overlapped")); err == nil {
		t.Fatal("two asks ran at once in one repository")
	}
}

// SPEC §9.6: one ask scope in the repository gives the whole chain, verified against its tiers
func TestPlace_OneFederatedAskGivesTheChain(t *testing.T) {
	repo, f := programme(t)
	f.union = true
	p := NewPlacer(f.ask).Place(context.Background(), repo, "data/thp101_pk.csv", "aaa")
	if len(p.Levels) != 3 || p.Levels[0].Name != "study-thp-101" || p.Levels[2].Name != "program-respiratory" || p.Levels[1].Source != "project-theophylline" || p.Note != "" {
		t.Fatalf("placement %+v", p)
	}
	if f.asked["scope"] != 1 {
		t.Errorf("asked scope %d times, want once", f.asked["scope"])
	}
}

func TestPlace_FederatedChainStopsAtAMissingOrUntrustedSeed(t *testing.T) {
	repo, f := programme(t)
	f.union = true
	f.distrust = map[string]bool{"sha256:programme": true}
	p := NewPlacer(f.ask).Place(context.Background(), repo, "a.csv", "aaa")
	if len(p.Levels) != 2 || !strings.Contains(p.Note, "no key trusted here") {
		t.Fatalf("untrusted programme: %+v", p)
	}
	f.distrust = nil
	delete(f.seeds, "program-respiratory")
	p = NewPlacer(f.ask).Place(context.Background(), repo, "a.csv", "aaa")
	if len(p.Levels) != 2 || !strings.Contains(p.Note, "no federated source holds its seed") {
		t.Fatalf("missing programme: %+v", p)
	}
	f.distrust = map[string]bool{"sha256:study": true}
	p = NewPlacer(f.ask).Place(context.Background(), repo, "a.csv", "aaa")
	if len(p.Levels) != 0 || !strings.Contains(p.Note, "no key trusted here") {
		t.Fatalf("untrusted study: %+v", p)
	}
}

// review of #23, C1: a child of the cockpit that keeps stdout open is not waited for
func TestCockpitAsk_AChildHoldingStdoutIsNotWaitedFor(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cockpit")
	os.WriteFile(exe, []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	t.Setenv("KTON_COCKPIT", exe)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := CockpitAsk(ctx, dir, map[string]string{"query": "producer"}); err == nil {
		t.Fatal("a cockpit that never answered gave no error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("waited %v", d)
	}
}
