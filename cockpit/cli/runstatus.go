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

func runStatus(ctx context.Context) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(cfg.RepoRoot, runsDir))
	if os.IsNotExist(err) || len(entries) == 0 {
		fmt.Println("no runs yet — cockpit run new <slug> --in FILE...   (or --from <dir> | sha256:<record>)")
		return nil
	}
	if err != nil {
		return err
	}
	c := cockpit.New(cockpit.Start{})
	fmt.Printf("%-24s %-26s %s\n", "run", "record", "claims")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := runsDir + "/" + e.Name()
		record, claims := "not run yet", ""
		if id, err := runFoton(ctx, cfg, rel); err == nil {
			record = short16(id)
			if ans, err := c.Ask(ctx, cockpit.AskRequest{Query: "about", Ref: id}); err == nil {
				names := map[string]string{}
				for _, t := range readTemplates(cfg) {
					names[t.Predicate] = t.Name
				}
				var said []string
				for _, cl := range ans.Claims {
					said = append(said, orDefault(names[cl.Predicate], cl.Predicate))
				}
				claims = strings.Join(said, ", ")
			}
		} else if outs, _ := filesUnderDir(cfg.RepoRoot, rel+"/out"); len(outs) > 0 {
			record = "outputs, no record"
		}
		fmt.Printf("%-24s %-26s %s\n", e.Name(), record, orDefault(claims, "—"))
		if theirs := reproducesOf(filepath.Join(cfg.RepoRoot, rel, "RUN.md")); theirs != "" {
			state := "not claimed yet: cockpit say reproduces " + rel
			if ans, err := c.Ask(ctx, cockpit.AskRequest{Query: "about", Ref: theirs}); err == nil {
				for _, cl := range ans.Claims {
					if strings.HasSuffix(cl.Predicate, "/reproduces") {
						state = "claimed"
					}
				}
			}
			fmt.Printf("%-24s reproduces %s — %s\n", "", short16(theirs), state)
		}
	}
	return nil
}
