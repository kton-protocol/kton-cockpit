package cli

// sync.go is `cockpit sync`: the one command for "bring this repository up to date with the others",
// whatever the backend. It replaces telling people to run `git pull` and `git push`, which only
// means something in git mode (ADR-011).
//
//	cockpit sync
//
// The backend takes in what others shared and shares what is durable here only (persist.share);
// then every federation source that can be pulled is.

import (
	"context"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func runSync(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: cockpit sync")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	mode := cfg.Raw.Repo.ModeOrDefault()
	if sy, ok := cfg.Backend.(backend.Syncer); ok {
		was, _ := sy.Head(ctx, cfg.Repo)
		now, err := sy.Sync(ctx, cfg.Repo)
		if err != nil {
			return fmt.Errorf("%s: %v\n(nothing was changed here)", mode, err)
		}
		state := "up to date"
		if was.ID != now.ID {
			state = "now at " + shortSHA(now.ID) + " (was " + orNone(shortSHA(was.ID)) + ")"
		}
		switch {
		case now.Rejected:
			state += "; sharing was refused: " + now.Reason
		case !cfg.Repo.Share:
			state += "; not shared (persist.share is off)"
		}
		fmt.Printf("%-12s %s\n", "repository", state)
	} else {
		fmt.Printf("%-12s %s mode keeps nothing shared — nothing to take in\n", "repository", mode)
	}
	if _, err := pullSources(ctx, cfg, nil); err != nil {
		return err
	}
	return nil
}
