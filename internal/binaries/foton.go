package binaries

import (
	"crypto/ed25519"
	"fmt"

	"kton.dev/plankton/core"
	ffoton "kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// foton/v1: nothing carried in the signed bytes (kton §6.6.1). The locators are signed separately
// and attached as material, and so is the concrete environment when a class is declared (ADR-010).

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

// envCarried says whether the record carries envRef beside it rather than covering it: with an
// environment class declared, the class is what the record covers, and the exact image is this
// producer's account of its own run (kton §6.5, kton#211, ADR-010).
func envCarried(in AuthorInput) bool { return in.Environment != "" && in.EnvRef != "" }

// attachEnv records this producer's concrete environment as its own signed statement beside the
// record (kton-env/v1). Another producer's statement stays theirs.
func attachEnv(reg *pregistry.Registry, id, envRef string, priv ed25519.PrivateKey) error {
	m, err := ffoton.SignEnv(id, envRef, priv)
	if err != nil {
		return fmt.Errorf("signing the environment: %w", err)
	}
	for _, have := range reg.Material(id) {
		if have.Material == m.Material {
			return nil
		}
	}
	if err := reg.AttachMaterial(pregistry.VerificationMaterial{Subject: m.Subject, Scheme: m.Scheme,
		MediaType: m.MediaType, Material: m.Material}); err != nil {
		return fmt.Errorf("attaching the environment: %w", err)
	}
	return nil
}

// carriedEnv reads the kton-env/v1 statements on a record that a key in keys signed, each only when
// that key also signed the record (ffoton.ReadEnv). Material of another scheme, or a statement that
// does not verify, says nothing here.
func carriedEnv(env core.Envelope, material []pregistry.VerificationMaterial, keys []ed25519.PublicKey) []RanIn {
	var out []RanIn
	seen := map[RanIn]bool{}
	for _, m := range material {
		if r, err := ffoton.ReadEnv(m.Scheme, m.Material, env, keys); err == nil && r != nil {
			if ri := (RanIn{EnvRef: r.EnvRef, Signer: r.Signer}); !seen[ri] {
				seen[ri] = true
				out = append(out, ri)
			}
		}
	}
	return out
}
