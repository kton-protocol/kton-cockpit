// Package verify resolves a record's trust tier from its actual verifying signature — never
// from the envelope's declared keyid/by field. This is the mechanical half of the governing
// spec's "verified, not declared" requirement; it adds no cryptography of its own, only calls
// `plankton verify` / `nekton verify` against each pubkey in the configured trust tiers.
package verify

import (
	"context"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// Kind distinguishes which binary's verify subcommand to use.
type Kind int

const (
	Foton Kind = iota
	Claim
)

// Untrusted is the tier name returned when a record does not verify against any pubkey configured
// in any trust tier. Callers must treat it as "exclude", not "lowest tier".
const Untrusted = ""

// ResolveTier tries every pubkey across every configured trust tier against idOrFile, in the
// order returned by config.TierPubkeys (map iteration order is unspecified, but every configured
// key is tried; the first one whose signature verifies wins). Returns Untrusted if no configured
// key verifies the record — the record is not forged, it is simply signed by someone outside this
// repo's trust config, and callers (ask) must exclude it rather than surface it unverified.
func ResolveTier(ctx context.Context, r *binaries.Runner, cfg *config.Config, idOrFile string, kind Kind) (tier string, verifyingKey string, err error) {
	for pubkey, tierName := range cfg.TierPubkeys() {
		var ok bool
		switch kind {
		case Foton:
			ok, _, err = r.VerifyFoton(ctx, idOrFile, pubkey)
		case Claim:
			ok, _, err = r.VerifyClaim(ctx, idOrFile, pubkey)
		}
		if err != nil {
			return Untrusted, "", err
		}
		if ok {
			return tierName, pubkey, nil
		}
	}
	return Untrusted, "", nil
}
