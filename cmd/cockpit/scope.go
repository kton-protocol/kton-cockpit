package main

// scope.go is the operator's side of scopes: opening one, sealing it into its parent, and checking
// one somebody sent you.
//
// None of it is a verb. A session chains claims into a scope the configuration already names; it
// cannot open one, cannot seal one, and cannot reach a registry other than its own. These three
// are work done at a desk, like `doctor` and `show`.
//
// There is deliberately no `share`. A scope IS one append-only file —
// registry/nekton/objects/<scope-id>.nekton.jsonl, with the seed as its first line — so handing one
// over is `cp`, and a subcommand whose whole content is resolving a name to a path would be
// ceremony. What the cockpit adds is on the RECEIVING side, where a foreign file has to be judged
// against this repository's own trust configuration; that is `scope read`.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
	"github.com/deathbychoco/claude-science-cockpit/internal/scope"
)

func runScope(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cockpit scope <seed|seal|read> ...")
	}
	switch args[0] {
	case "seed":
		return scopeSeed(ctx, args[1:])
	case "seal":
		return scopeSeal(ctx, args[1:])
	case "read":
		return scopeRead(ctx, args[1:])
	default:
		return fmt.Errorf("unknown scope command %q (seed, seal, read)", args[0])
	}
}

// scopeSeed opens a scope and prints the line to paste into the configuration. It does NOT edit the
// config: `cockpit.config.json` is the one file a session cannot reach, and a tool that rewrote it
// would make the ceiling something a process changes rather than something a person sets.
func scopeSeed(ctx context.Context, args []string) error {
	var name, parent string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--parent":
			i++
			if i >= len(args) {
				return fmt.Errorf("--parent expects the name of a scope this repo configures, or a scope id")
			}
			parent = args[i]
		default:
			if name != "" {
				return fmt.Errorf("seed takes one name, got %q and %q", name, args[i])
			}
			name = args[i]
		}
	}
	if name == "" {
		return fmt.Errorf("usage: cockpit scope seed <name> [--parent <configured-name|scope-id>]")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	parentID, err := resolveScopeRef(cfg, parent)
	if err != nil {
		return err
	}

	id, err := binaries.New(cfg).SeedScope(ctx, name, cfg.NektonKey, parentID)
	if err != nil {
		return err
	}
	if _, err := gitops.CommitAndPush(ctx, cfg, []string{cfg.Raw.Paths.NektonDir}, "scope: seed "+name); err != nil {
		return fmt.Errorf("the scope was opened but committing it failed: %w", err)
	}

	fmt.Printf("opened scope %q\n  %s\n", name, id)
	if parentID != "" {
		fmt.Printf("  under parent %s — `cockpit scope seal %s` records its head there\n", parentID, name)
	} else {
		fmt.Printf("  with no parent, so it cannot be sealed. A seal records this scope's head in a\n" +
			"  parent chain, which is what makes a later rewind detectable; a scope that wants one\n" +
			"  must name its parent at birth, because the seed covers it.\n")
	}
	fmt.Printf("\nAdd it to cockpit.config.json so claims may name it:\n")
	fmt.Printf("  \"claims\": { \"scopes\": { %q: %q } }\n", name, id)
	return nil
}

// scopeSeal records the scope's current head in its parent.
//
// Repeatable on purpose. Each seal fixes a point the chain can no longer be rewound behind: the
// parent now carries that head, so dropping the tail afterwards yields a chain whose head no longer
// matches what was recorded. Sealing twice is not a mistake, it is the ratchet — seal after the
// rules are written, seal again after the work is done.
func scopeSeal(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cockpit scope seal <configured-scope-name>")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	child, configured, ok := cfg.ScopeID(args[0])
	if !ok {
		return fmt.Errorf("no claim scope named %q in this repo's config; it has: %s",
			args[0], strings.Join(configured, ", "))
	}

	r := binaries.New(cfg)
	seed, err := r.Seed(ctx, child)
	if err != nil {
		return err
	}
	if seed.Parent == "" {
		return fmt.Errorf("scope %q (%s) names no parent, so there is nowhere to record its head.\n"+
			"  A seed covers its parent, so this cannot be added afterwards — the scope would have to be\n"+
			"  opened again with --parent, and its existing claims belong to the old one", args[0], child)
	}
	childHead, err := r.Head(ctx, child)
	if err != nil {
		return err
	}
	parentHead, err := r.Head(ctx, seed.Parent)
	if err != nil {
		return fmt.Errorf("scope %q names parent %s, which this registry does not hold: %w", args[0], seed.Parent, err)
	}
	if len(childHead.Heads) > 1 {
		fmt.Fprintf(os.Stderr, "warning: %q has %d heads, so this seal fixes ONE branch and says nothing\n"+
			"  about the others. `cockpit_ask` query \"scope\" reports them.\n", args[0], len(childHead.Heads))
	}
	if childHead.Unresolved > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d claim(s) in %q have a predecessor this registry does not hold,\n"+
			"  so the head being sealed is the current KNOWN one and may not be the chain's last.\n",
			childHead.Unresolved, args[0])
	}

	claimID, err := r.SealScope(ctx, child, childHead.Heads[0], seed.Parent, parentHead.Heads[0], cfg.NektonKey)
	if err != nil {
		return err
	}
	if _, err := gitops.CommitAndPush(ctx, cfg, []string{cfg.Raw.Paths.NektonDir}, "scope: seal "+args[0]); err != nil {
		return fmt.Errorf("the seal was recorded but committing it failed: %w", err)
	}

	fmt.Printf("sealed %q at %s\n", args[0], childHead.Heads[0])
	fmt.Printf("  %d claim(s) are now fixed: a chain rewound behind this head no longer matches what\n"+
		"  the parent carries.\n", childHead.ChainLength)
	fmt.Printf("  recorded in parent %s as %s\n", seed.Parent, claimID)
	return nil
}

// scopeRead judges a scope somebody sent, against THIS repository's trust configuration, without
// letting any of it into this repository's own registry.
//
// A scope is one file and that file is a registry's whole storage for it, so the received file is
// read where it lies — copied into a directory of its own, read, thrown away. Nothing is ingested:
// a reader of someone else's review is not thereby making statements in their own store.
func scopeRead(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cockpit scope read <received-scope-file.nekton.jsonl>")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	received, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	// The scope's identity comes from the file's CONTENTS, not from its name. A received file is a
	// thing somebody emailed you; it may well arrive as review.jsonl, and a registry keyed by
	// whatever the sender's filesystem happened to call it would be a registry keyed by a guess.
	// The seed is a record in the file and its claim id IS the scope id, so the file says who it is.
	ids, err := claimIDsIn(received)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("%s holds no readable records; a scope file is one JSON record per line", args[0])
	}

	dir, err := os.MkdirTemp("", "cockpit-received-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	objects := filepath.Join(dir, "objects", "scope")
	if err := os.MkdirAll(objects, 0o755); err != nil {
		return err
	}
	// The format marker travels from this repo's own registry: it says which layout wrote the file,
	// and a reader that guessed would be the stale-binary failure in a new place.
	if b, rerr := os.ReadFile(filepath.Join(cfg.NektonDir, "objects", ".format")); rerr == nil {
		_ = os.WriteFile(filepath.Join(dir, "objects", ".format"), b, 0o600)
	}

	// A registry files a scope under its id, so the id has to be known before the file is placed.
	// Each record in the file is a candidate; the seed is the one that reports genesis. In practice
	// it is the first line, so this loop runs once — it exists so a file that arrived out of order
	// is read rather than refused.
	foreign := *cfg
	foreign.NektonDir = dir
	run := binaries.New(&foreign)
	var scopeID string
	for _, candidate := range ids {
		path := filepath.Join(objects, strings.TrimPrefix(candidate, "sha256:")+".nekton.jsonl")
		if werr := os.WriteFile(path, received, 0o600); werr != nil {
			return werr
		}
		if _, serr := run.Seed(ctx, candidate); serr == nil {
			scopeID = candidate
			break
		}
		if rerr := os.Remove(path); rerr != nil {
			return rerr
		}
	}
	if scopeID == "" {
		return fmt.Errorf("%s holds %d record(s) but none of them is a scope seed, so this is not a scope file",
			args[0], len(ids))
	}

	// The same verdict a session gets from `ask`, computed against a registry that is only this
	// file — and against THIS repository's trust tiers, which is the whole reason the cockpit is
	// involved rather than `cat`.
	verdict, _, err := scope.Describe(ctx, &foreign, run, scopeID)
	if err != nil {
		return err
	}
	fmt.Println(verdict.Line())
	fmt.Printf("\nnothing was ingested: this file was read where it lies and the copy is gone.\n")
	return nil
}

// resolveScopeRef turns a --parent argument into a scope id: either a name this repo configures, or
// an id given outright. Empty stays empty.
func resolveScopeRef(cfg *config.Config, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if strings.HasPrefix(ref, "sha256:") {
		return ref, nil
	}
	id, configured, ok := cfg.ScopeID(ref)
	if !ok {
		return "", fmt.Errorf("no claim scope named %q in this repo's config; it has: %s",
			ref, strings.Join(configured, ", "))
	}
	return id, nil
}

// claimIDsIn reads the record ids out of a scope file, in the order they appear.
//
// Tolerant of a line it cannot parse rather than refusing the file: a scope file is append-only and
// a torn last write is a real thing, and losing the whole review because one line is short would
// be a worse answer than reading the rest and letting the seal verdict report the gap.
func claimIDsIn(file []byte) ([]string, error) {
	var ids []string
	for _, line := range strings.Split(string(file), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			ClaimID string `json:"claimId"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.ClaimID == "" {
			continue
		}
		ids = append(ids, rec.ClaimID)
	}
	return ids, nil
}
