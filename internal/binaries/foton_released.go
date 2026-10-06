//go:build !unreleased

package binaries

import (
	"crypto/ed25519"

	ffoton "kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// The published kernel (kton v0.2.1) has no foton/v1: the locators are carried in the record's own
// file entries, as in cockpit v0.2.0, and nothing is attached beside it.

func recordFile(path, hash string, uris []string) ffoton.FileSpec {
	return ffoton.FileSpec{Path: path, Hash: hash, URI: uris}
}

func withStatement(*ffoton.Spec) {}

func attachLocators(*pregistry.Registry, string, []ffoton.FileSpec, []ffoton.FileSpec,
	map[string][]string, ed25519.PrivateKey) error {
	return nil
}
