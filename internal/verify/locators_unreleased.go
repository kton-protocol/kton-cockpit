//go:build unreleased

package verify

import (
	"crypto/ed25519"

	"kton.dev/plankton/core"
	ffoton "kton.dev/plankton/foton"
)

type signedLocator struct {
	Path, Hash, Signer string
	URI                []string
}

// readLocators reads one piece of kton-locator/v1 material; any other scheme yields nothing.
func readLocators(scheme, material string, f core.Foton, keys []ed25519.PublicKey) ([]signedLocator, error) {
	if scheme != ffoton.LocatorScheme {
		return nil, nil
	}
	ls, err := ffoton.ReadLocators(scheme, material, f, keys)
	if err != nil {
		return nil, err
	}
	out := make([]signedLocator, 0, len(ls))
	for _, l := range ls {
		out = append(out, signedLocator{Path: l.Path, Hash: l.Hash, Signer: l.Signer, URI: l.URI})
	}
	return out, nil
}
