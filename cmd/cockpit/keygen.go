package main

// keygen.go makes a signing identity.
//
// It exists because the alternative was telling people to fetch a kernel binary and run
// `plankton keygen`, which is a second tool to install and a second thing to get wrong before the
// first record is written. The kernels are linked in; generating the key they will verify is a
// function call.
//
//	cockpit keygen alice        writes keys/alice.key + keys/alice.pub
//	                                   keys/alice-claims.key + .pub
//
// Two keys, because a foton and a claim are signed by different halves of an identity, and the
// configuration names both. That is the kernel's split, not this tool's.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func runKeygen(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("usage: cockpit keygen <name>      (e.g. cockpit keygen alice)")
	}
	name := args[0]
	if filepath.Base(name) != name {
		return fmt.Errorf("a key name is a name, not a path: %q", name)
	}

	cfg, err := config.Load(ctx, ".")
	keysDir := "keys"
	root := "."
	if err == nil {
		keysDir, root = cfg.KeysDir, cfg.RepoRoot
	} else {
		// Before there is a configuration there is still a key to make — `init` cannot bind a repo
		// to an identity that does not exist yet. So a missing config is not an error here.
		keysDir = filepath.Join(root, "keys")
	}
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return err
	}

	for _, suffix := range []string{"", "-claims"} {
		stem := filepath.Join(keysDir, name+suffix)
		if _, serr := os.Stat(stem + ".key"); serr == nil {
			fmt.Printf("  %s.key exists, keeping it\n", rel(root, stem))
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
		fmt.Printf("  %s.key  (PRIVATE — anyone who can read it can sign as you)\n", rel(root, stem))
		fmt.Printf("  %s.pub\n", rel(root, stem))

		// Said rather than assumed: on a Windows drive mounted into WSL the mode does not stick,
		// and a key that looks protected and is not is worse than one that never claimed to be.
		if fi, serr := os.Stat(stem + ".key"); serr == nil && fi.Mode().Perm()&0o077 != 0 {
			fmt.Printf("      readable by others (mode %v) — this filesystem does not enforce modes\n", fi.Mode().Perm())
		}
	}
	return nil
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}
