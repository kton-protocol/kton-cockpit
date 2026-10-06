//go:build !unreleased

package verify

import (
	"crypto/ed25519"

	"kton.dev/plankton/core"
)

type signedLocator struct {
	Path, Hash, Signer string
	URI                []string
}

// The published kernel (kton v0.2.1) has no kton-locator/v1: a record carries its locators itself,
// so no material yields any.
func readLocators(string, string, core.Foton, []ed25519.PublicKey) ([]signedLocator, error) {
	return nil, nil
}
