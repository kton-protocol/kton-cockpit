package cli

// runstatus.go is `cockpit run list` with where each run stands: whether its run is a record, the
// claims about it, and — for a run made from somebody's record — which one it reproduces and
// whether that has been claimed.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// Run is one entry of `run list --json`: a run folder, the record its run made (empty when it has
// not run, with HasOutputs when it has outputs but no record of its own), the templates of the
// claims about that record, and the record it reproduces when it was made with run new --from.
type Run struct {
	Slug       string   `json:"slug"`
	Dir        string   `json:"dir"`
	Record     string   `json:"record,omitempty"`
	HasOutputs bool     `json:"hasOutputs,omitempty"`
	Claims     []string `json:"claims"`
	Reproduces string   `json:"reproduces,omitempty"`
	Claimed    bool     `json:"reproductionClaimed,omitempty"`
}

func listRuns(ctx context.Context, cfg *config.Config) ([]Run, error) {
	out := []Run{}
	entries, err := os.ReadDir(filepath.Join(cfg.RepoRoot, runsRel(cfg)))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	c := cockpit.New(cockpit.Start{})
	names := map[string]string{}
	for _, t := range readTemplates(cfg) {
		names[t.Predicate] = t.Name
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := runsRel(cfg) + "/" + e.Name()
		run := Run{Slug: e.Name(), Dir: rel, Claims: []string{}}
		if id, err := runFoton(ctx, cfg, rel); err == nil {
			run.Record = id
			if ans, err := c.Ask(ctx, cockpit.AskRequest{Query: "about", Ref: id}); err == nil {
				for _, cl := range ans.Claims {
					run.Claims = append(run.Claims, orDefault(names[cl.Predicate], cl.Predicate))
				}
			}
		} else if outs, _ := filesUnderDir(cfg.RepoRoot, rel+"/out"); len(outs) > 0 {
			run.HasOutputs = true
		}
		if theirs := reproducesOf(filepath.Join(cfg.RepoRoot, rel, "RUN.md")); theirs != "" {
			run.Reproduces = theirs
			if ans, err := c.Ask(ctx, cockpit.AskRequest{Query: "about", Ref: theirs}); err == nil {
				for _, cl := range ans.Claims {
					if strings.HasSuffix(cl.Predicate, "/reproduces") {
						run.Claimed = true
					}
				}
			}
		}
		out = append(out, run)
	}
	return out, nil
}

func runStatus(ctx context.Context, asJSON bool) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	runs, err := listRuns(ctx, cfg)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(runs)
	}
	if len(runs) == 0 {
		fmt.Println("no runs yet — cockpit run new <slug> --in FILE...   (or --from <dir> | sha256:<record>)")
		return nil
	}
	fmt.Printf("%-24s %-26s %s\n", "run", "record", "claims")
	for _, r := range runs {
		record := "not run yet"
		switch {
		case r.Record != "":
			record = short16(r.Record)
		case r.HasOutputs:
			record = "outputs, no record"
		}
		fmt.Printf("%-24s %-26s %s\n", r.Slug, record, orDefault(strings.Join(r.Claims, ", "), "—"))
		if r.Reproduces != "" {
			state := "not claimed yet: cockpit say reproduces " + r.Dir
			if r.Claimed {
				state = "claimed"
			}
			fmt.Printf("%-24s reproduces %s — %s\n", "", short16(r.Reproduces), state)
		}
	}
	return nil
}
