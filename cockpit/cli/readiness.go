package cli

// readiness.go is the end of `doctor`: whether this repository is ready for the three verbs, and if
// not, the next steps in the order they are needed. The lines above it say what is configured; this
// says what that means for the person who just ran `init`.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
	"github.com/kton-protocol/kton-cockpit/internal/show"
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
	if cfg.Identity.SessionID == "" {
		steps = append(steps, nextStep{true, fmt.Sprintf("this checkout has no signing identity (%s) — run `cockpit keygen <name>`",
			config.Rel("local/"+config.IdentityFile))})
	}
	for _, k := range []struct{ path, what string }{
		{cfg.PlanktonKey, "fotons"}, {cfg.NektonKey, "claims"},
	} {
		if k.path == "" {
			continue
		}
		pub, err := publicOf(k.path)
		if err != nil {
			steps = append(steps, nextStep{true, fmt.Sprintf("no signing key for %s at %s — run `cockpit keygen <name>`",
				k.what, k.path)})
			continue
		}
		if !trusted[pub] {
			steps = append(steps, nextStep{true, fmt.Sprintf("this repository's own %s will not verify here: no trust tier holds the public half of %s (`cockpit keygen` names the line to add)",
				k.what, rel(cfg.RepoRoot, k.path))})
		}
	}

	// A template say serves: the repository's own, or one an admitted package brings (SPEC §9.11).
	// When that set cannot be read, say refuses every template, so doctor says that once rather than
	// name a missing file per template (kton-review N1 on #180).
	offered, offeredErr := binaries.New(cfg).Templates()
	if offeredErr != nil && len(cfg.Raw.Claims.AllowedTemplates) > 0 {
		steps = append(steps, nextStep{false, fmt.Sprintf("`say` will refuse every template: the vocabulary cannot be read (%v)", offeredErr)})
	}
	for _, t := range cfg.Raw.Claims.AllowedTemplates {
		if offeredErr != nil {
			break
		}
		if _, ok := offered.Get(t); ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.TemplatesDir, t+".json")); err != nil {
			steps = append(steps, nextStep{false, fmt.Sprintf("`say` with template %s will be refused: no %s/%s.json",
				t, rel(cfg.RepoRoot, cfg.TemplatesDir), t)})
		}
	}

	// What the store holds, asked of the backend. One that cannot say (local mode) has nothing a
	// peer could read either, so there is nothing to warn about.
	if insp, ok := cfg.Backend.(backend.Inspector); ok {
		keys := privateKeyFiles(cfg)
		want := append([]string{config.Rel(config.ConfigFile)}, tierFilesIn(cfg)...)
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

	// An admitted package signed by a key of the trusted ring: show leaves its label out, ring wins.
	if _, collisions, err := show.Packages(cfg); err == nil {
		for _, c := range collisions {
			steps = append(steps, nextStep{false, c})
		}
	}

	if why := binaries.MisplacedAliases(cfg); why != "" {
		steps = append(steps, nextStep{true, why})
	}
	if !cfg.Raw.Execution.Enabled() {
		steps = append(steps, nextStep{false, "optional: `cockpit pin <image>` runs every publish in a digest-pinned container, so the environment is observed, not asserted"})
	}
	// An index daemon near its memory limit stops answering (userinterface, 21.11.: a window federating
	// twelve registries at 2 GiB of 2 GiB): said before it does.
	if cfg.Raw.Index.Kind == "oxigraph" {
		if p, limit, ok := oxigraph.MemoryInUse(ctx, cfg); ok && p >= 85 {
			steps = append(steps, nextStep{false, fmt.Sprintf("the index daemon uses %.0f%% of its %s of memory: raise index.memory (or the machine's index memory file), then cockpit index stop; the next query starts it with more", p, limit)})
		}
	}

	// The machine's shared index: what it runs with and where that came from, and a memory file that is
	// not used (cockpit-runtime, 20.11.).
	if cfg.Raw.Index.Kind == "oxigraph" && oxigraph.SharedIndex(cfg) {
		running, from, problem := oxigraph.SharedMemory(cfg)
		if problem != "" {
			steps = append(steps, nextStep{false, problem})
		}
		if running != "" {
			if from == "" {
				from = "not recorded: started by an older cockpit"
			}
			steps = append(steps, nextStep{false, fmt.Sprintf("the machine's shared index runs with %s of memory (from %s)", running, from)})
		}
	}

	// The admitted packages that contribute nothing, and the names offered only qualified: the rest
	// of the vocabulary works, so these are said, not blocking.
	if voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages, cfg.Raw.Claims.AllowedModules); err == nil {
		for _, r := range voc.Refused {
			steps = append(steps, nextStep{false, fmt.Sprintf("the allowed package %s contributes nothing: %s", orDefault(r.Name, r.Package), r.Reason)})
		}
		for _, c := range voc.Clashes {
			steps = append(steps, nextStep{false, c})
		}
		for _, c := range binaries.New(cfg).TemplateClashes() {
			steps = append(steps, nextStep{false, c})
		}
	}
	return steps
}

// printReadiness ends the text output, and returns errNotReady when a step marked ! is left.
func printReadiness(ctx context.Context, cfg *config.Config) error {
	steps := readiness(ctx, cfg)
	blocking := blocked(steps)
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
	if blocking {
		return errNotReady
	}
	return nil
}

func blocked(steps []nextStep) bool {
	for _, s := range steps {
		if s.must {
			return true
		}
	}
	return false
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

// privateKeyFiles are private key files found anywhere under .kton/, repo-relative. There should be
// none (ADR-016: keys live in ~/.kton-keys); any that is persisted is reported as leaked.
func privateKeyFiles(cfg *config.Config) []string {
	var out []string
	filepath.WalkDir(cfg.KtonDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".key") {
			out = append(out, filepath.ToSlash(rel(cfg.RepoRoot, p)))
		}
		return nil
	})
	return out
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// checkMadePath is checkPath for a directory publish or say creates when it first writes.
func checkMadePath(p string) string {
	if pathState(p) != stateOK {
		return p + "  [made on first write]"
	}
	return p + "  [ok]"
}
