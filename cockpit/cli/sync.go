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
	asJSON := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !asJSON {
		return fmt.Errorf("usage: cockpit sync [--json]")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	out := []Synced{}
	emit := printSynced
	if asJSON {
		emit = func(s Synced) { out = append(out, s) }
	}
	mode := cfg.Raw.Repo.ModeOrDefault()
	repo := Synced{Name: "repository"}
	if sy, ok := cfg.Backend.(backend.Syncer); ok {
		was, _ := sy.Head(ctx, cfg.Repo)
		now, err := sy.Sync(ctx, cfg.Repo)
		if err != nil {
			return fmt.Errorf("%s: %v\n(nothing was changed here)", mode, err)
		}
		repo.Was, repo.Now = was.ID, now.ID
		switch {
		case now.Rejected:
			repo.Note = "sharing was refused: " + now.Reason
		case !cfg.Repo.Share:
			repo.Note = "not shared (persist.share is off)"
		}
	} else {
		repo.Note = string(mode) + " mode keeps nothing shared — nothing to take in"
	}
	emit(repo)
	if _, err := pullSourcesTo(ctx, cfg, nil, emit); err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	return nil
}
