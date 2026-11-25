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
)

const federationUsage = "usage: cockpit federation add <name> <git-URL|checkout> [--path DIR] | list | pull [name] | remove <name>"

func runFederation(ctx context.Context, args []string) error {
	if len(args) == 0 || (len(args) == 1 && args[0] == "--json") {
		return federationList(ctx, len(args) == 1)
	}
	switch args[0] {
	case "add":
		return federationAdd(ctx, args[1:])
	case "list":
		return federationList(ctx, hasFlag(args[1:], "--json"))
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
	if fi, err := os.Stat(filepath.Join(where, config.Dir, "plankton")); err == nil && fi.IsDir() && sub == "" {
		abs, _ := filepath.Abs(where)
		block = map[string]string{"name": name, "kind": "dir", "path": abs}
	} else if sub != "" {
		block["path"] = sub
	}

	// Added means read fresh: a cache left by an earlier source of this name is not it.
	if cache, err := binaries.SourceCache(cfg, name); err == nil {
		os.RemoveAll(cache)
	}
	cfgPath := config.ConfigPath(cfg.RepoRoot)
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
		return fmt.Errorf("%v\n(.kton/cockpit.config.json is unchanged)", err)
	}
	fmt.Printf("source %s added (%s)\n", name, block["kind"])
	describeSource(ctx, fresh, name)
	fmt.Println("\ncommit .kton/cockpit.config.json to keep it; `cockpit federation pull` reads newer records")
	return nil
}

// federationListing is `federation list --json` and serve's federation/list: every configured source with
// what it holds now.
func federationListing(ctx context.Context, cfg *config.Config) []SourceStatus {
	out := []SourceStatus{}
	for _, s := range cfg.Raw.Federation.Sources {
		out = append(out, sourceStatus(ctx, cfg, s.Name))
	}
	return out
}

func federationList(ctx context.Context, asJSON bool) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(federationListing(ctx, cfg))
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
	n, err := pullSources(ctx, cfg, args)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no source to pull%s", map[bool]string{true: " named " + strings.Join(args, ""), false: ""}[len(args) > 0])
	}
	return nil
}

// pullSources refreshes every configured source whose kind can be pulled (only the one named, if a
// name is given), says where each now is, and returns how many it pulled. A kind read in place has
// nothing to refresh and is skipped.
func pullSources(ctx context.Context, cfg *config.Config, args []string) (int, error) {
	return pullSourcesTo(ctx, cfg, args, printSynced)
}

// Synced is one line of `sync`: the repository or a source, the revision it was at and is at now.
type Synced struct {
	Name string `json:"name"`
	Was  string `json:"was,omitempty"`
	Now  string `json:"now,omitempty"`
	Note string `json:"note,omitempty"`
}

func printSynced(s Synced) {
	state := "up to date"
	if s.Was != s.Now {
		state = "now at " + shortSHA(s.Now) + " (was " + orNone(shortSHA(s.Was)) + ")"
	}
	if s.Note != "" {
		if s.Now == "" && s.Was == "" {
			state = s.Note
		} else {
			state += "; " + s.Note
		}
	}
	fmt.Printf("%-12s %s\n", s.Name, state)
}

func pullSourcesTo(ctx context.Context, cfg *config.Config, args []string, emit func(Synced)) (int, error) {
	n := 0
	for _, s := range cfg.Raw.Federation.Sources {
		if len(args) > 0 && s.Name != args[0] {
			continue
		}
		k, _ := source.Lookup(s.Kind)
		p, ok := k.(source.Puller)
		if !ok {
			continue
		}
		cache, err := binaries.SourceCache(cfg, s.Name)
		if err != nil {
			return n, err
		}
		was, now, err := p.Pull(ctx, s, cfg.RepoRoot, cache)
		if err != nil {
			return n, fmt.Errorf("source %s: %v", s.Name, err)
		}
		emit(Synced{Name: s.Name, Was: was, Now: now})
		n++
	}
	return n, nil
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
	if err := editConfig(config.ConfigPath(cfg.RepoRoot), set([]string{"federation", "sources"}, keep)); err != nil {
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
// SourceStatus is one entry of `federation list --json`: a configured source, where it was read,
// and how much of it this repository's trust tiers verify.
type SourceStatus struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	URL             string `json:"url,omitempty"`
	Path            string `json:"path,omitempty"`
	ReadAt          string `json:"readAt,omitempty"`
	Problem         string `json:"problem,omitempty"`
	Records         int    `json:"records"`
	Claims          int    `json:"claims"`
	VerifiedRecords int    `json:"verifiedRecords"`
	VerifiedClaims  int    `json:"verifiedClaims"`
}

func sourceStatus(ctx context.Context, cfg *config.Config, name string) SourceStatus {
	var spec source.Spec
	for _, s := range cfg.Raw.Federation.Sources {
		if s.Name == name {
			spec = s
		}
	}
	var b map[string]string
	spec.Decode(&b)
	st := SourceStatus{Name: name, Kind: spec.Kind, URL: b["url"], Path: b["path"]}
	cache, _ := binaries.SourceCache(cfg, name)
	st.ReadAt, _ = source.Head(ctx, cache)
	k, ok := source.Lookup(spec.Kind)
	if !ok {
		st.Problem = "no source kind " + spec.Kind
		return st
	}
	regs, err := k.Fetch(ctx, spec, cfg.RepoRoot, cache)
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	keys, _ := binaries.TrustKeys(cfg, "")
	signers := map[string]bool{}
	for _, reg := range regs {
		if binaries.Exists(reg.PlanktonDir) {
			if p, err := binaries.OpenPlanktonSource(reg.PlanktonDir); err == nil {
				for _, r := range p.Records(0) {
					st.Records++
					st.VerifiedRecords += verifiedBy(r.Envelope, keys, signers)
				}
			}
		}
		if binaries.Exists(reg.NektonDir) {
			if n, err := binaries.OpenNektonSource(reg.NektonDir); err == nil {
				for _, r := range n.Records(0) {
					st.Claims++
					st.VerifiedClaims += verifiedBy(r.Envelope, keys, signers)
				}
			}
		}
	}
	return st
}

func describeSource(ctx context.Context, cfg *config.Config, name string) {
	st := sourceStatus(ctx, cfg, name)
	fmt.Printf("%-12s %s %s", name, st.Kind, orDefault(st.URL, st.Path))
	if st.Path != "" && st.Kind == "git" {
		fmt.Printf(" (under %s)", st.Path)
	}
	fmt.Println()
	if st.ReadAt != "" {
		fmt.Printf("             read at %s\n", st.ReadAt)
	}
	if strings.HasPrefix(st.Problem, "no source kind") {
		return
	}
	if st.Problem != "" {
		fmt.Printf("             cannot be read: %s\n", st.Problem)
		return
	}
	fmt.Printf("             %d record(s), %d claim(s); your trust tiers verify %d and %d\n", st.Records, st.Claims, st.VerifiedRecords, st.VerifiedClaims)
	if st.Records+st.Claims > st.VerifiedRecords+st.VerifiedClaims {
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
