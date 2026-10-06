package cli

// federation.go is the operator's side of federation.sources: reading other registries — an
// aggregation repository above all — together with this repository's own.
//
//	cockpit federation add <name> <git-URL|checkout> [--path DIR]
//	cockpit federation list
//	cockpit federation pull [name]
//	cockpit federation remove <name>
//
// A source adds records, never signers: what it holds counts only where a key in this repository's
// trust tiers verifies it, so `add` and `list` say how much of it that is.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
	nregistry "kton.dev/nekton/registry"
	pregistry "kton.dev/plankton/registry"
)

const federationUsage = "usage: cockpit federation add <name> <git-URL|checkout> [--path DIR] | list | pull [name] | remove <name>"

func runFederation(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return federationList(ctx)
	}
	switch args[0] {
	case "add":
		return federationAdd(ctx, args[1:])
	case "list":
		return federationList(ctx)
	case "pull":
		return federationPull(ctx, args[1:])
	case "remove":
		return federationRemove(ctx, args[1:])
	}
	return fmt.Errorf(federationUsage)
}

func federationAdd(ctx context.Context, args []string) error {
	var pos []string
	sub := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" && i+1 < len(args) {
			sub = args[i+1]
			i++
			continue
		}
		pos = append(pos, args[i])
	}
	if len(pos) != 2 || strings.HasPrefix(pos[0], "-") {
		return fmt.Errorf(federationUsage)
	}
	name, where := pos[0], pos[1]
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	for _, s := range cfg.Raw.Federation.Sources {
		if s.Name == name {
			return fmt.Errorf("a source %q exists already: cockpit federation remove %s first", name, name)
		}
	}

	// A checkout on disk with a participant's layout is read in place; anything else is cloned.
	block := map[string]string{"name": name, "kind": "git", "url": where}
	if fi, err := os.Stat(filepath.Join(where, "registry")); err == nil && fi.IsDir() && sub == "" {
		abs, _ := filepath.Abs(where)
		block = map[string]string{"name": name, "kind": "dir", "path": abs}
	} else if sub != "" {
		block["path"] = sub
	}

	// Added means read fresh: a cache left by an earlier source of this name is not it.
	if cache, err := binaries.SourceCache(cfg, name); err == nil {
		os.RemoveAll(cache)
	}
	cfgPath := filepath.Join(cfg.RepoRoot, "cockpit.config.json")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var specs []json.RawMessage
	for _, s := range cfg.Raw.Federation.Sources {
		specs = append(specs, s.Block)
	}
	nb, _ := json.Marshal(block)
	specs = append(specs, nb)
	if err := editConfig(cfgPath, set([]string{"federation", "sources"}, specs)); err != nil {
		return err
	}
	// Read once now: a source that cannot be read is said here, and the configuration is put back,
	// rather than every ask failing from now on.
	fresh, err := config.Load(ctx, ".")
	if err == nil {
		_, _, err = binaries.ReadDirs(ctx, fresh)
	}
	if err != nil {
		os.WriteFile(cfgPath, before, 0o644)
		return fmt.Errorf("%v\n(cockpit.config.json is unchanged)", err)
	}
	fmt.Printf("source %s added (%s)\n", name, block["kind"])
	describeSource(ctx, fresh, name)
	fmt.Println("\ncommit cockpit.config.json to keep it; `cockpit federation pull` reads newer records")
	return nil
}

func federationList(ctx context.Context) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if len(cfg.Raw.Federation.Sources) == 0 {
		fmt.Println("no sources — this repository reads its own records only. Add one: cockpit federation add <name> <git-URL>")
		return nil
	}
	for i, s := range cfg.Raw.Federation.Sources {
		if i > 0 {
			fmt.Println()
		}
		describeSource(ctx, cfg, s.Name)
	}
	return nil
}

func federationPull(ctx context.Context, args []string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	n := 0
	for _, s := range cfg.Raw.Federation.Sources {
		if (len(args) > 0 && s.Name != args[0]) || s.Kind != "git" {
			continue
		}
		var b struct {
			URL string `json:"url"`
		}
		s.Decode(&b)
		cache, err := binaries.SourceCache(cfg, s.Name)
		if err != nil {
			return err
		}
		was, _ := source.Head(ctx, cache)
		now, err := source.Pull(ctx, b.URL, cache)
		if err != nil {
			return fmt.Errorf("source %s: %v", s.Name, err)
		}
		state := "up to date"
		if was != now {
			state = "now at " + shortSHA(now) + " (was " + orNone(shortSHA(was)) + ")"
		}
		fmt.Printf("%-12s %s\n", s.Name, state)
		n++
	}
	if n == 0 {
		return fmt.Errorf("no git source to pull%s", map[bool]string{true: " named " + strings.Join(args, ""), false: ""}[len(args) > 0])
	}
	return nil
}

func federationRemove(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cockpit federation remove <name>")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	var keep []json.RawMessage
	found := false
	for _, s := range cfg.Raw.Federation.Sources {
		if s.Name == args[0] {
			found = true
			continue
		}
		keep = append(keep, s.Block)
	}
	if !found {
		return fmt.Errorf("no source %q", args[0])
	}
	if keep == nil {
		keep = []json.RawMessage{}
	}
	if err := editConfig(filepath.Join(cfg.RepoRoot, "cockpit.config.json"), set([]string{"federation", "sources"}, keep)); err != nil {
		return err
	}
	if cache, err := binaries.SourceCache(cfg, args[0]); err == nil {
		os.RemoveAll(cache)
	}
	fmt.Printf("source %s removed; its records are no longer read here\n", args[0])
	return nil
}

// describeSource says where a source is, which commit it was read at, and how many of its records a
// key in this repository's trust tiers verifies.
func describeSource(ctx context.Context, cfg *config.Config, name string) {
	var spec source.Spec
	for _, s := range cfg.Raw.Federation.Sources {
		if s.Name == name {
			spec = s
		}
	}
	var b map[string]string
	spec.Decode(&b)
	fmt.Printf("%-12s %s %s", name, spec.Kind, orDefault(b["url"], b["path"]))
	if b["path"] != "" && spec.Kind == "git" {
		fmt.Printf(" (under %s)", b["path"])
	}
	fmt.Println()
	cache, _ := binaries.SourceCache(cfg, name)
	if head, _ := source.Head(ctx, cache); head != "" {
		fmt.Printf("             read at %s\n", head)
	}
	k, ok := source.Lookup(spec.Kind)
	if !ok {
		return
	}
	regs, err := k.Fetch(ctx, spec, cfg.RepoRoot, cache)
	if err != nil {
		fmt.Printf("             cannot be read: %v\n", err)
		return
	}
	keys, _ := binaries.TrustKeys(cfg, "")
	var fotons, fv, claims, cv int
	signers := map[string]bool{}
	for _, reg := range regs {
		if _, err := os.Stat(reg.PlanktonDir); err == nil {
			if p, err := pregistry.Open(reg.PlanktonDir); err == nil {
				for _, r := range p.Records(0) {
					fotons++
					fv += verifiedBy(r.Envelope, keys, signers)
				}
			}
		}
		if _, err := os.Stat(reg.NektonDir); err == nil {
			if n, err := nregistry.Open(reg.NektonDir); err == nil {
				for _, r := range n.Records(0) {
					claims++
					cv += verifiedBy(r.Envelope, keys, signers)
				}
			}
		}
	}
	fmt.Printf("             %d record(s), %d claim(s); your trust tiers verify %d and %d\n", fotons, claims, fv, cv)
	if fotons+claims > fv+cv {
		fmt.Printf("             the rest count only once you trust their signers: cockpit trust add <tier> <their repo>\n")
	}
}

func verifiedBy(env core.Envelope, keys []ed25519.PublicKey, signers map[string]bool) int {
	if id := core.VerifiedSignerKeyID(env, keys); id != "" {
		signers[id] = true
		return 1
	}
	return 0
}
