package cockpit

import (
	"context"
	"crypto/ed25519"
	"time"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/keyload"
)

// previousOwn is what a template with "replace": "previous" names in replaces (ADR-013 A1/A5): the
// newest claim of that template about the same subject that the signing key itself signed and has not
// replaced since. "" when there is none, or the template does not replace. It never names another
// key's claim: what a different signer said is not this signer's to retire.
func previousOwn(ctx context.Context, cfg *config.Config, r *binaries.Runner, template, subject, signKey string) (string, error) {
	set, err := r.Templates()
	if err != nil {
		return "", err
	}
	t, ok := set.Get(template)
	if !ok || t.Replace != "previous" {
		return "", nil
	}
	me, pub, err := keyOf(signKey)
	if err != nil {
		return "", err
	}
	claims, err := r.About(ctx, subject)
	if err != nil {
		return "", err
	}
	pred := set.Resolve(t.Predicate)
	var mine []ownClaim
	replaced := map[string]bool{}
	for _, c := range claims {
		// Signed by this key, verified: a declared keyid is not covered by the signature, so a claim
		// that only names this keyid is not this signer's (kton-review P1 on #166).
		if c.Predicate != pred || core.VerifiedSignerKeyID(c.Envelope, []ed25519.PublicKey{pub}) != me {
			continue
		}
		if obj, ok := c.Object.(map[string]any); ok {
			if rep, ok := obj["replaces"].(string); ok {
				if n, ok := core.NormalizeContentHash(rep); ok {
					replaced[n] = true
				}
			}
		}
		when, _ := time.Parse(time.RFC3339Nano, c.When)
		mine = append(mine, ownClaim{c.ID, when})
	}
	return newestUnreplaced(mine, replaced), nil
}

// ownClaim is one of the signer's own claims: its id and signed time.
type ownClaim struct {
	id   string
	when time.Time
}

// newestUnreplaced is the newest claim of mine that none of mine replaces, by signed time, then id.
func newestUnreplaced(mine []ownClaim, replaced map[string]bool) string {
	best := ownClaim{}
	for _, c := range mine {
		if replaced[c.id] {
			continue
		}
		if best.id == "" || c.when.After(best.when) || (c.when.Equal(best.when) && c.id > best.id) {
			best = c
		}
	}
	return best.id
}

// keyOf is the key id and public key of the private key at path.
func keyOf(path string) (string, ed25519.PublicKey, error) {
	pub, err := keyload.PublicKey(expandHome(path))
	if err != nil {
		return "", nil, err
	}
	return core.KeyIDHex(pub), pub, nil
}
