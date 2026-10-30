package cockpit

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
)

// identityName is a key's name in the keyring: no path, nothing hidden.
var identityName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// keyAs is the claim key say signs with when a request names who it acts as (SPEC §8.7):
// <name>-claims.key in identity.keyring, whose public half beside it must match it and be in a
// trust tier. The operator fixes the set by what the tiers trust; the request picks one. It returns
// a refusal's reason, or the key's path.
func keyAs(cfg *config.Config, name string) (string, string) {
	ring := cfg.Raw.Identity.Keyring
	switch {
	case ring == "":
		return "", "this repository configures no identity.keyring, so say signs as its own nekton_key only"
	case !identityName.MatchString(name):
		return "", fmt.Sprintf("%q is not a key name (letters, digits, '.', '_', '-')", name)
	case cfg.Raw.Anchor.Enabled:
		return "", "say --as with anchor.enabled is not supported yet: the anchor entry would name this repository's key, not the signer's"
	}
	key := filepath.Join(expandHome(ring), name+"-claims.key")
	b, err := os.ReadFile(key)
	if err != nil {
		return "", fmt.Sprintf("no key %s-claims.key in identity.keyring: %v", name, err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", fmt.Sprintf("%s is not a signing key", key)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	pb, err := os.ReadFile(strings.TrimSuffix(key, ".key") + ".pub")
	if err != nil {
		return "", fmt.Sprintf("the public half of %s is not beside it: %v", name, err)
	}
	if strings.TrimSpace(string(pb)) != hex.EncodeToString(pub) {
		return "", fmt.Sprintf("%s-claims.pub is not the public half of %s-claims.key", name, name)
	}
	tier, err := verify.TierOfKey(cfg, pub)
	if err != nil {
		return "", err.Error()
	}
	if tier == "" {
		return "", fmt.Sprintf("%s is in no trust tier of this repository: a claim signed by it would be excluded from every answer", name)
	}
	return key, ""
}
