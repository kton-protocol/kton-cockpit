// Package verify resolves a record's trust tier from its actual verifying signature — never from
// the envelope's declared keyid/by field. This is the mechanical half of the "verified, not
// declared" requirement (SPEC §9.1).
//
// It adds no cryptography of its own. `core.VerifiedSignerKeyID` is the kernel's own answer to
// exactly this question — "which of these keys signed this, if any" — and the tier is then a lookup
// from the keyid it returns.
//
// This used to shell out to `plankton verify` / `nekton verify` once per configured key per record,
// which is O(keys × records) processes for one query and was the single largest cost in the test
// suite. It also meant reading a three-way exit code to learn something the library returns as a
// value. What the exit codes distinguished — wrong key, tampered bytes, structurally unstorable —
// matters to a person at a terminal; here the question is only whether a key this repository
// configured signed this record, and the kernel answers that in one call.
package verify

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
)

// Kind distinguishes which substrate holds the record.
type Kind int

const (
	Foton Kind = iota
	Claim
)

// Untrusted is the tier name returned when a record verifies against no configured key. Callers
// must treat it as "exclude", not "lowest tier".
const Untrusted = ""

// ResolveTier reports the trust tier of the key that ACTUALLY signed idOrFile, and that key's
// configured path. idOrFile is a record id held in this repo's registry, or a path to a DSSE
// envelope on disk — the second case is how a claim is verified when its predecessor is not held
// here, since such a claim is stored but is not retrievable by id.
//
// Returns Untrusted when no configured key verifies it. That is not a forged record: it is one
// signed by somebody outside this repository's trust configuration, and SPEC §9.1 requires callers to
// exclude it rather than surface it unverified.
func ResolveTier(ctx context.Context, r *binaries.Runner, cfg *config.Config, idOrFile string, kind Kind) (tier string, verifyingKey string, err error) {
	env, err := envelopeOf(ctx, r, idOrFile, kind)
	if err != nil {
		return Untrusted, "", err
	}

	// Loaded per call rather than cached: the trust configuration is the ceiling, and a cockpit that
	// remembered it would answer from a config that is no longer on disk. Config.Load already runs
	// on every tool call for the same reason.
	keys, byKeyID, err := tierKeys(cfg)
	if err != nil {
		return Untrusted, "", err
	}
	keyid := core.VerifiedSignerKeyID(env, keys)
	if keyid == "" {
		return Untrusted, "", nil
	}
	found := byKeyID[keyid]
	return found.tier, found.path, nil
}

type tierKey struct{ tier, path string }

// tierKeys reads every configured pubkey once and indexes it by keyid, which is what the kernel
// returns. A key that will not parse is an ERROR, never a silent skip: a typo'd path in a trust
// tier that quietly dropped one key would narrow what this repo trusts without saying so, and
// "this signer is not trusted" would be reported for a signer that is.
func tierKeys(cfg *config.Config) ([]ed25519.PublicKey, map[string]tierKey, error) {
	var keys []ed25519.PublicKey
	byKeyID := map[string]tierKey{}
	for path, tier := range cfg.TierPubkeys() {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("trust tier %q names %s, which cannot be read: %w", tier, path, err)
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, nil, fmt.Errorf("trust tier %q names %s, which is not a public key: %w", tier, path, err)
		}
		keys = append(keys, pub)
		byKeyID[core.KeyIDHex(pub)] = tierKey{tier: tier, path: path}
	}
	return keys, byKeyID, nil
}

// envelopeOf returns the signed bytes to check, from the registry by id or from a file.
func envelopeOf(ctx context.Context, r *binaries.Runner, idOrFile string, kind Kind) (core.Envelope, error) {
	if _, ok := core.NormalizeContentHash(idOrFile); !ok {
		b, err := os.ReadFile(idOrFile)
		if err != nil {
			return core.Envelope{}, fmt.Errorf("%q is neither a content hash nor a readable envelope: %w", idOrFile, err)
		}
		var env core.Envelope
		if jerr := json.Unmarshal(b, &env); jerr != nil {
			return core.Envelope{}, fmt.Errorf("%s is not a DSSE envelope: %w", idOrFile, jerr)
		}
		return env, nil
	}
	raw, err := r.EnvelopeFor(ctx, idOrFile)
	if err != nil {
		return core.Envelope{}, err
	}
	var env core.Envelope
	if jerr := json.Unmarshal(raw, &env); jerr != nil {
		return core.Envelope{}, fmt.Errorf("the stored envelope for %s is not readable: %w", idOrFile, jerr)
	}
	return env, nil
}
