//go:build unreleased

package binaries

import (
	"crypto/ed25519"
	"fmt"

	ffoton "kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// foton/v1: nothing carried in the signed bytes (kton §6.6.1). The locators are signed separately
// and attached as material.

func recordFile(path, hash string, _ []string) ffoton.FileSpec {
	return ffoton.FileSpec{Path: path, Hash: hash}
}

func withStatement(spec *ffoton.Spec) { spec.Statement = "v1" }

// attachLocators records this repo's locators as its own signed statement beside the record
// (kton §8.1, kton-locator/v1). Another producer's locators stay theirs; ours do not replace them.
func attachLocators(reg *pregistry.Registry, id string, inputs, outputs []ffoton.FileSpec,
	located map[string][]string, priv ed25519.PrivateKey) error {
	var locFiles []ffoton.FileSpec
	for _, f := range append(append([]ffoton.FileSpec{}, inputs...), outputs...) {
		if uris := located[f.Path]; len(uris) > 0 {
			locFiles = append(locFiles, ffoton.FileSpec{Path: f.Path, Hash: f.Hash, URI: uris})
		}
	}
	if len(locFiles) == 0 {
		return nil
	}
	m, err := ffoton.SignLocators(id, locFiles, priv)
	if err != nil {
		return fmt.Errorf("signing the locators: %w", err)
	}
	for _, have := range reg.Material(id) {
		if have.Material == m.Material { // Ed25519 is deterministic: same statement, same bytes
			return nil
		}
	}
	if err := reg.AttachMaterial(pregistry.VerificationMaterial{Subject: m.Subject, Scheme: m.Scheme,
		MediaType: m.MediaType, Material: m.Material}); err != nil {
		return fmt.Errorf("attaching the locators: %w", err)
	}
	return nil
}
