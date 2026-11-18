package cli

// keygen.go makes a signing identity.
//
// It exists because the alternative was telling people to fetch a kernel binary and run
// `plankton keygen`, which is a second tool to install and a second thing to get wrong before the
// first record is written. The kernels are linked in; generating the key they will verify is a
// function call.
//
//	cockpit keygen alice        writes ~/.kton-keys/alice.key + alice.pub
//	                                   ~/.kton-keys/alice-claims.key + .pub
//
// Two keys, because a foton and a claim are signed by different halves of an identity. That is the
// kernel's split, not this tool's. The private halves never go into a repository (ADR-016): in a
// repository, keygen copies the public halves into tier self (.kton/keys/self/) and, when this
// checkout has no identity yet, writes .kton/local/identity.json.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func runKeygen(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		// A leading dash is a flag someone tried, not a name: `keygen --help` used to write
		// keys/--help.key.
		return fmt.Errorf("usage: cockpit keygen <name>      (e.g. cockpit keygen alice)")
	}
	name := args[0]
	if filepath.Base(name) != name {
		return fmt.Errorf("a key name is a name, not a path: %q", name)
	}

	cfg, err := config.Load(ctx, ".")
	if _, serr := os.Stat(config.ConfigPath(".")); err != nil && serr == nil {
		// A configuration that is there and does not load is not "no configuration yet": the keys
		// would land unbound, and the reason would go unsaid.
		return err
	}
	ring := keysDir()
	if err == nil && config.InsideDir(cfg.RepoRoot, ring) {
		return fmt.Errorf("the key folder %s is inside the repository %s; private keys never are (ADR-016) — set KTON_KEYS elsewhere", ring, cfg.RepoRoot)
	}
	if err := os.MkdirAll(ring, 0o700); err != nil {
		return err
	}

	for _, suffix := range []string{"", "-claims"} {
		stem := filepath.Join(ring, name+suffix)
		if _, serr := os.Stat(stem + ".key"); serr == nil {
			fmt.Printf("  %s.key exists, keeping it\n", stem)
			continue
		}
		pub, priv, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return gerr
		}
		// The private half is written as the 32-byte SEED in hex, which is the form the kernel
		// reads. Writing the 64-byte expanded key would look equivalent and would not load.
		if werr := os.WriteFile(stem+".key", []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600); werr != nil {
			return werr
		}
		if werr := os.WriteFile(stem+".pub", []byte(hex.EncodeToString(pub)+"\n"), 0o644); werr != nil {
			return werr
		}
		fmt.Printf("  %s.key  (PRIVATE — anyone who can read it can sign as you)\n", stem)
		fmt.Printf("  %s.pub\n", stem)

		// Said rather than assumed: on a Windows drive mounted into WSL the mode does not stick,
		// and a key that looks protected and is not is worse than one that never claimed to be.
		if fi, serr := os.Stat(stem + ".key"); serr == nil && fi.Mode().Perm()&0o077 != 0 {
			fmt.Printf("      readable by others (mode %v) — this filesystem does not enforce modes\n", fi.Mode().Perm())
		}
	}
	if err != nil {
		return nil // no repository here: the keys are made, and `init` binds them later
	}
	if cfg.Identity.SessionID != "" && cfg.Identity.SessionID != name {
		pubs, perr := publishPublicKeys(cfg.RepoRoot, name, ring, "self")
		if perr != nil {
			return perr
		}
		fmt.Printf("\n  public keys %s (commit them)\n", strings.Join(pubs, ", "))
		fmt.Printf("  this checkout still signs as %s; to sign as %s, edit %s\n", cfg.Identity.SessionID, name,
			config.Rel("local/"+config.IdentityFile))
		return nil
	}
	pubs, berr := bindIdentity(cfg.RepoRoot, name, ring)
	if berr != nil {
		return berr
	}
	if !slices.Contains(cfg.Raw.Trust.Tiers, "self") {
		if err := editConfig(config.ConfigPath(cfg.RepoRoot), set([]string{"trust", "tiers"}, append(slices.Clone(cfg.Raw.Trust.Tiers), "self"))); err != nil {
			return err
		}
	}
	fmt.Printf("\n  this checkout signs as %s (%s), trusted in tier self\n", name, config.Rel("local/"+config.IdentityFile))
	fmt.Printf("  public keys %s — commit them, a peer verifies with them\n", strings.Join(pubs, ", "))
	return nil
}
