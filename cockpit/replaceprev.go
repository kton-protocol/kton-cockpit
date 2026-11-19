package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
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
	me, err := keyIDOfKey(signKey)
	if err != nil {
		return "", err
	}
	claims, err := r.About(ctx, subject)
	if err != nil {
		return "", err
	}
	pred := set.Resolve(t.Predicate)
	type own struct {
		id   string
		when time.Time
	}
	var mine []own
	replaced := map[string]bool{}
	for _, c := range claims {
		signed := false
		for _, k := range c.SignatureKeyIDs {
			signed = signed || k == me
		}
		if !signed || c.Predicate != pred {
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
		mine = append(mine, own{c.ID, when})
	}
	best := own{}
	for _, c := range mine {
		if replaced[c.id] {
			continue
		}
		if best.id == "" || c.when.After(best.when) || (c.when.Equal(best.when) && c.id > best.id) {
			best = c
		}
	}
	return best.id, nil
}

// keyIDOfKey is the key id of the private key at path.
func keyIDOfKey(path string) (string, error) {
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return "", err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", fmt.Errorf("%s is not a signing key", path)
	}
	return core.KeyIDHex(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)), nil
}
