package cli

// readiness.go is the end of `doctor`: whether this repository is ready for the three verbs, and if
// not, the next steps in the order they are needed. The lines above it say what is configured; this
// says what that means for the person who just ran `init`.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

type nextStep struct {
	must bool // the verbs fail or mislead until this is done
	text string
}

func readiness(ctx context.Context, cfg *config.Config) []nextStep {
	var steps []nextStep

	// The signing keys, and whether the repository trusts their public halves. Compared by key,
	// not by file name: a tier entry pointing at a copy is as good as one pointing at the original.
	trusted := map[string]bool{}
	for p := range cfg.TierPubkeys() {
		if b, err := os.ReadFile(p); err == nil {
			trusted[strings.TrimSpace(string(b))] = true
		}
	}
	for _, k := range []struct{ path, what string }{
		{cfg.PlanktonKey, "fotons"}, {cfg.NektonKey, "claims"},
	} {
		pub, err := publicOf(k.path)
		if err != nil {
			steps = append(steps, nextStep{true, fmt.Sprintf("no signing key for %s at %s — run `cockpit keygen <name>` and set identity as it says",
				k.what, rel(cfg.RepoRoot, k.path))})
			continue
		}
		if !trusted[pub] {
			steps = append(steps, nextStep{true, fmt.Sprintf("this repository's own %s will not verify here: no trust tier holds the public half of %s (`cockpit keygen` names the line to add)",
				k.what, rel(cfg.RepoRoot, k.path))})
		}
	}

	for _, t := range cfg.Raw.Claims.AllowedTemplates {
		if _, err := os.Stat(filepath.Join(cfg.TemplatesDir, t+".json")); err != nil {
			steps = append(steps, nextStep{false, fmt.Sprintf("`say` with template %s will be refused: no %s/%s.json",
				t, rel(cfg.RepoRoot, cfg.TemplatesDir), t)})
		}
	}

	// What the store holds, asked of the backend. One that cannot say (local mode) has nothing a
	// peer could read either, so there is nothing to warn about.
	if insp, ok := cfg.Backend.(backend.Inspector); ok {
		keys := privateKeyFiles(cfg)
		want := append([]string{"cockpit.config.json"}, tierFilesIn(cfg)...)
		if st, err := insp.Status(ctx, cfg.Repo, append(keys, want...)); err == nil {
			var leaked, missing []string
			for _, p := range keys {
				if st[p] != backend.Untracked {
					leaked = append(leaked, p)
				}
			}
			for _, p := range want {
				if st[p] == backend.Untracked {
					missing = append(missing, p)
				}
			}
			if len(leaked) > 0 {
				steps = append(steps, nextStep{true, fmt.Sprintf("PRIVATE keys are persisted (%s) — remove them from the store's history and make new ones",
					strings.Join(leaked, ", "))})
			}
			if len(missing) > 0 {
				steps = append(steps, nextStep{false, fmt.Sprintf("not persisted yet: %s — a peer needs them to verify your records (%s backend: persist and share them)",
					strings.Join(missing, ", "), cfg.Raw.Repo.ModeOrDefault())})
			}
		}
	}

	if !cfg.Raw.Execution.Enabled() {
		steps = append(steps, nextStep{false, "optional: `cockpit pin <image>` runs every publish in a digest-pinned container, so the environment is observed, not asserted"})
	}
	return steps
}

func printReadiness(ctx context.Context, cfg *config.Config) {
	steps := readiness(ctx, cfg)
	blocking := false
	for _, s := range steps {
		blocking = blocking || s.must
	}
	if !blocking {
		fmt.Printf("\nready:          publish · say · ask\n")
	} else {
		fmt.Printf("\nnot ready:      the verbs fail or mislead until the steps marked ! are done\n")
	}
	for i, s := range steps {
		mark := " "
		if s.must {
			mark = "!"
		}
		fmt.Printf("  %s %d. %s\n", mark, i+1, s.text)
	}
}

// publicOf is the hex public key of a private key file in the form the kernel reads (a hex seed).
func publicOf(keyPath string) (string, error) {
	b, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", fmt.Errorf("%s is not a hex seed", keyPath)
	}
	return hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)), nil
}

// tierFilesIn are the configured public keys that live inside the repository, repo-relative.
func tierFilesIn(cfg *config.Config) []string {
	var out []string
	for p := range cfg.TierPubkeys() {
		r, err := filepath.Rel(cfg.RepoRoot, p)
		if err != nil || strings.HasPrefix(r, "..") {
			continue
		}
		out = append(out, filepath.ToSlash(r))
	}
	sort.Strings(out)
	return out
}

// privateKeyFiles are the private key files on disk in keys_dir, repo-relative.
func privateKeyFiles(cfg *config.Config) []string {
	ents, err := os.ReadDir(cfg.KeysDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".key" {
			out = append(out, filepath.ToSlash(filepath.Join(cfg.Raw.Paths.KeysDir, e.Name())))
		}
	}
	return out
}

// checkMadePath is checkPath for a directory publish or say creates when it first writes.
func checkMadePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p + "  [made on first write]"
	}
	return p + "  [ok]"
}
