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
// made by someone who has seen them. What it removes is the hand work uat/e2e.sh used to do in
// Python: copying .pub files in under a name that cannot overwrite this repository's own, and
// editing trust.tiers.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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

	keys, where, err := peerKeys(ctx, src, publicKeysDir(&cfg.Raw))
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
	for p, t := range cfg.TierPubkeys() {
		if b, err := os.ReadFile(p); err == nil && t != tier {
			inTier[strings.TrimSpace(string(b))] = t
		}
	}

	dst := publicKeysDir(&cfg.Raw)
	entries := append([]string{}, cfg.Raw.Trust.Tiers[tier]...)
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
		// Prefixed with the tier unless it already is: two parties both calling theirs
		// session-1.pub must not land on one file.
		file := k.name
		if !strings.HasPrefix(file, tier) {
			file = tier + "-" + file
		}
		rel := path.Join(dst, file)
		to := filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel))
		if have, err := os.ReadFile(to); err == nil && strings.TrimSpace(string(have)) != k.hex {
			return fmt.Errorf("%s exists with a different key — not overwritten; remove it or use another tier name", rel)
		}
		state := "new"
		if slices.Contains(entries, rel) {
			state = "already in tier " + tier
		} else {
			entries = append(entries, rel)
			writes = append(writes, write{to, k.hex})
		}
		fmt.Printf("  %-28s keyid %s  → %s  (%s)\n", k.name, shortID(k.keyID), rel, state)
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
	if err := editConfig(filepath.Join(cfg.RepoRoot, "cockpit.config.json"), set([]string{"trust", "tiers", tier}, entries)); err != nil {
		return err
	}
	fmt.Printf("\ntier %s: %d key(s) added. Commit %s/ and cockpit.config.json.\n", tier, len(writes), dst)
	return nil
}

// peerKeys reads the public keys a peer publishes, from a checkout on disk or from a git URL at its
// current commit, and says where they were read.
func peerKeys(ctx context.Context, src, keysDir string) ([]peerKey, string, error) {
	dir := filepath.Join(src, filepath.FromSlash(keysDir))
	where := "the working tree"
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		tmp, err := os.MkdirTemp("", "cockpit-trust-")
		if err != nil {
			return nil, "", err
		}
		defer os.RemoveAll(tmp)
		if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-checkout", src, tmp).CombinedOutput(); err != nil {
			return nil, "", fmt.Errorf("%s is neither a checkout with %s/ nor a git repository: %s", src, keysDir, strings.TrimSpace(string(out)))
		}
		if out, err := exec.CommandContext(ctx, "git", "-C", tmp, "checkout", "--quiet", "HEAD", "--", keysDir).CombinedOutput(); err != nil {
			return nil, "", fmt.Errorf("%s publishes no %s/ at its HEAD: %s", src, keysDir, strings.TrimSpace(string(out)))
		}
		sha, _ := exec.CommandContext(ctx, "git", "-C", tmp, "rev-parse", "HEAD").Output()
		where = "commit " + string(bytes.TrimSpace(sha))
		dir = filepath.Join(tmp, filepath.FromSlash(keysDir))
	} else if sha, err := exec.CommandContext(ctx, "git", "-C", src, "rev-parse", "HEAD").Output(); err == nil {
		where = "the working tree at " + string(bytes.TrimSpace(sha))
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
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
			return nil, "", fmt.Errorf("%s/%s is not a public key", keysDir, e.Name())
		}
		out = append(out, peerKey{e.Name(), h, core.KeyIDHex(pub)})
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("%s has no public keys in %s/", src, keysDir)
	}
	return out, where, nil
}

func trustList(ctx context.Context) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	tiers := make([]string, 0, len(cfg.Raw.Trust.Tiers))
	for t := range cfg.Raw.Trust.Tiers {
		tiers = append(tiers, t)
	}
	sort.Strings(tiers)
	for _, t := range tiers {
		fmt.Printf("%s\n", t)
		if len(cfg.Raw.Trust.Tiers[t]) == 0 {
			fmt.Println("  (no keys — nothing counts in this tier)")
		}
		for _, p := range cfg.Raw.Trust.Tiers[t] {
			abs := p
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(cfg.RepoRoot, p)
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				fmt.Printf("  %-40s MISSING — this entry trusts nothing\n", p)
				continue
			}
			pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
			if err != nil {
				fmt.Printf("  %-40s NOT A PUBLIC KEY\n", p)
				continue
			}
			fmt.Printf("  %-40s keyid %s\n", p, shortID(core.KeyIDHex(pub)))
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
	if _, ok := cfg.Raw.Trust.Tiers[tier]; !ok {
		return fmt.Errorf("no tier %q", tier)
	}
	if tier == "self" {
		return fmt.Errorf("tier self holds this repository's own keys; without it its own records do not verify here")
	}
	tiers := map[string][]string{}
	for t, ks := range cfg.Raw.Trust.Tiers {
		if t != tier {
			tiers[t] = ks
		}
	}
	if err := editConfig(filepath.Join(cfg.RepoRoot, "cockpit.config.json"), set([]string{"trust", "tiers"}, tiers)); err != nil {
		return err
	}
	fmt.Printf("tier %s removed; records only its keys signed no longer count here. The .pub files stay in place.\n", tier)
	return nil
}

func shortID(id string) string {
	if len(id) > 16 {
		return id[:16]
	}
	return id
}
