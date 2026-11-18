package cli

// trust.go is the operator's side of trust tiers: taking another party's public keys in, and seeing
// which tiers hold which keys.
//
//	cockpit trust add <tier> <source> [--yes]   source: a peer's checkout, or a git URL
//	cockpit trust list
//	cockpit trust remove <tier>
//
// It decides nothing. Which tier a key belongs in is the operator's call, and `add` without --yes
// only shows what would be trusted — the keyids, and the commit they were read at — so the call is
// made by someone who has seen them. A tier is a folder (ADR-016): trusting a key is copying its public
// half into .kton/keys/<tier>/ and naming the tier in trust.tiers.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
)

func runTrust(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cockpit trust add <tier> <source> [--yes] | list | remove <tier>")
	}
	switch args[0] {
	case "add":
		return trustAdd(ctx, args[1:])
	case "list":
		return trustList(ctx)
	case "remove":
		return trustRemove(ctx, args[1:])
	}
	return fmt.Errorf("usage: cockpit trust add <tier> <source> [--yes] | list | remove <tier>")
}

type peerKey struct {
	name  string // file name in the source
	hex   string
	keyID string
}

func trustAdd(ctx context.Context, args []string) error {
	yes := false
	var pos []string
	for _, a := range args {
		if a == "--yes" {
			yes = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 2 || strings.HasPrefix(pos[0], "-") {
		return fmt.Errorf("usage: cockpit trust add <tier> <source> [--yes]\n" +
			"  <source> is a peer's checkout, or a git URL (https://github.com/<owner>/<repo>)")
	}
	tier, src := pos[0], pos[1]
	if tier == "self" {
		return fmt.Errorf("tier self is this repository's own keys (keygen puts them there); name the peer's tier for what it is")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}

	keys, where, err := peerKeys(ctx, cfg, src)
	if err != nil {
		return err
	}
	own := map[string]bool{}
	for _, k := range []string{cfg.PlanktonKey, cfg.NektonKey} {
		if pub, err := publicOf(k); err == nil {
			own[pub] = true
		}
	}
	// A key already in another tier stays where the operator put it: one key in two tiers would
	// make which tier a record counts in depend on lookup order.
	inTier := map[string]string{}
	have := map[string]bool{} // the keys this tier's folder holds already
	for _, t := range cfg.Raw.Trust.Tiers {
		for _, p := range cfg.TierKeys(t) {
			if b, err := os.ReadFile(p); err == nil {
				if t == tier {
					have[strings.TrimSpace(string(b))] = true
				} else {
					inTier[strings.TrimSpace(string(b))] = t
				}
			}
		}
	}

	dst := cfg.TierDir(tier)
	type write struct{ to, hex string }
	var writes []write
	fmt.Printf("%s, read at %s:\n", src, where)
	for _, k := range keys {
		if own[k.hex] {
			fmt.Printf("  %-28s %s  this repository's own key — skipped\n", k.name, shortID(k.keyID))
			continue
		}
		if t, ok := inTier[k.hex]; ok {
			fmt.Printf("  %-28s %s  already trusted in tier %s — skipped\n", k.name, shortID(k.keyID), t)
			continue
		}
		if have[k.hex] {
			fmt.Printf("  %-28s keyid %s  (already in tier %s)\n", k.name, shortID(k.keyID), tier)
			continue
		}
		// Two parties both calling theirs session-1.pub must not land on one file: a name taken by
		// another key gets the keyid's start appended.
		file := k.name
		to := filepath.Join(dst, file)
		if b, err := os.ReadFile(to); err == nil && strings.TrimSpace(string(b)) != k.hex {
			file = strings.TrimSuffix(file, ".pub") + "-" + shortID(k.keyID)[:8] + ".pub"
			to = filepath.Join(dst, file)
		}
		writes = append(writes, write{to, k.hex})
		have[k.hex] = true
		fmt.Printf("  %-28s keyid %s  → %s  (new)\n", k.name, shortID(k.keyID), config.Rel("keys/"+tier+"/"+file))
	}
	if len(writes) == 0 {
		fmt.Println("nothing to add")
		return nil
	}
	if !yes {
		fmt.Printf("\nrecords these keys signed will count in tier %s. If that is right:\n  cockpit trust add %s %s --yes\n", tier, tier, src)
		return nil
	}
	for _, w := range writes {
		if err := os.MkdirAll(filepath.Dir(w.to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(w.to, []byte(w.hex+"\n"), 0o644); err != nil {
			return err
		}
	}
	if !slices.Contains(cfg.Raw.Trust.Tiers, tier) {
		tiers := append(slices.Clone(cfg.Raw.Trust.Tiers), tier)
		if err := editConfig(config.ConfigPath(cfg.RepoRoot), set([]string{"trust", "tiers"}, tiers)); err != nil {
			return err
		}
	}
	fmt.Printf("\ntier %s: %d key(s) added. Commit %s/ and %s.\n", tier, len(writes), config.Rel("keys/"+tier), config.Rel(config.ConfigFile))
	return nil
}

// peerKeys reads a peer's own public keys — its tier self, .kton/keys/self/ — through the federation
// source kind that reads that kind of place, and says where they were read. src is a configured
// federation source by name (read through its own kind, whatever kind that is), a checkout on disk
// (read in place), or else a git repository to clone. A peer's other tiers are the keys it trusts,
// not its own, and are never taken under its name.
func peerKeys(ctx context.Context, cfg *config.Config, src string) ([]peerKey, string, error) {
	selfDir := filepath.Join(config.Dir, "keys", "self")
	var spec source.Spec
	base := "."
	for _, s := range cfg.Raw.Federation.Sources {
		if s.Name == src {
			spec, base = s, cfg.RepoRoot
		}
	}
	if spec.Kind == "" {
		block := map[string]string{"name": "peer", "kind": "git", "url": src}
		if fi, err := os.Stat(filepath.Join(src, selfDir)); err == nil && fi.IsDir() {
			abs, _ := filepath.Abs(src)
			block = map[string]string{"name": "peer", "kind": "dir", "path": abs}
		}
		b, _ := json.Marshal(block)
		if err := json.Unmarshal(b, &spec); err != nil {
			return nil, "", err
		}
	}
	k, ok := source.Lookup(spec.Kind)
	co, can := k.(source.Checkouter)
	if !ok || !can {
		return nil, "", fmt.Errorf("this build cannot read a %s source as files", spec.Kind)
	}
	cache, err := os.MkdirTemp("", "cockpit-trust-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(cache)
	root, rev, err := co.Checkout(ctx, spec, base, cache)
	if err != nil {
		return nil, "", fmt.Errorf("%s is neither a checkout with %s/ nor a repository this build can read: %v", src, filepath.ToSlash(selfDir), err)
	}
	where := "the working tree"
	if rev != "" {
		where = "revision " + rev
	}
	dir := filepath.Join(root, selfDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("%s publishes no %s/ (read at %s)", src, filepath.ToSlash(selfDir), where)
	}
	var out []peerKey
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".pub" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, "", err
		}
		h := strings.TrimSpace(string(b))
		pub, err := core.ParsePublicKeyHex(h)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, "", fmt.Errorf("%s/%s is not a public key", filepath.ToSlash(selfDir), e.Name())
		}
		out = append(out, peerKey{e.Name(), h, core.KeyIDHex(pub)})
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("%s has no public keys of its own in %s/", src, filepath.ToSlash(selfDir))
	}
	return out, where + " — its own keys (tier self)", nil
}

func trustList(ctx context.Context) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	tiers := slices.Clone(cfg.Raw.Trust.Tiers)
	sort.Strings(tiers)
	for _, t := range tiers {
		fmt.Printf("%s  (%s/)\n", t, config.Rel("keys/"+t))
		keys := cfg.TierKeys(t)
		if len(keys) == 0 {
			fmt.Println("  (no keys — nothing counts in this tier)")
		}
		for _, p := range keys {
			name := filepath.Base(p)
			b, err := os.ReadFile(p)
			if err != nil {
				fmt.Printf("  %-40s UNREADABLE\n", name)
				continue
			}
			pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
			if err != nil {
				fmt.Printf("  %-40s NOT A PUBLIC KEY\n", name)
				continue
			}
			fmt.Printf("  %-40s keyid %s\n", name, shortID(core.KeyIDHex(pub)))
		}
	}
	return nil
}

func trustRemove(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cockpit trust remove <tier>")
	}
	tier := args[0]
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if !slices.Contains(cfg.Raw.Trust.Tiers, tier) {
		return fmt.Errorf("no tier %q", tier)
	}
	if tier == "self" {
		return fmt.Errorf("tier self holds this repository's own keys; without it its own records do not verify here")
	}
	tiers := slices.DeleteFunc(slices.Clone(cfg.Raw.Trust.Tiers), func(t string) bool { return t == tier })
	if err := editConfig(config.ConfigPath(cfg.RepoRoot), set([]string{"trust", "tiers"}, tiers)); err != nil {
		return err
	}
	fmt.Printf("tier %s removed; records only its keys signed no longer count here. %s/ stays in place.\n", tier, config.Rel("keys/"+tier))
	return nil
}

func shortID(id string) string {
	if len(id) > 16 {
		return id[:16]
	}
	return id
}
