package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The single most important case in this file is the one that must PASS: an unlisted scheme.
// Everything else here refuses something, and a validator that grew one more refusal — of evidence
// it simply had not heard of — would quietly turn this repo's scheme list into a protocol version,
// which is exactly what SPEC §8.1 exists to prevent.
func TestValidateMaterial_AnUnlistedSchemeIsAccepted(t *testing.T) {
	err := validateMaterial(Material{Attach: []Attachment{{
		Scheme: "acme-internal-badge-v3", MediaType: "application/octet-stream", File: "identity/badge.bin",
	}}})
	if err != nil {
		t.Fatalf("an unlisted scheme must be accepted, not rejected: %v", err)
	}
}

func TestValidateMaterial_RefusesWhatCannotBeCarriedSafely(t *testing.T) {
	cases := map[string]struct {
		in   Material
		want string
	}{
		"no scheme": {
			Material{Attach: []Attachment{{MediaType: "application/json", File: "a.json"}}},
			"no scheme",
		},
		"no media type": {
			Material{Attach: []Attachment{{Scheme: "rekor-entry", File: "a.json"}}},
			"mediaType",
		},
		"no file": {
			Material{Attach: []Attachment{{Scheme: "rekor-entry", MediaType: "application/json"}}},
			"names no file",
		},
		// A private key attached as evidence would be stored with the record and shared with it.
		"a private key": {
			Material{Attach: []Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "keys/session-1.key"}}},
			"*.key",
		},
		// Evidence is committed alongside the record, so a path outside the repository would attach
		// bytes nobody receiving the record can see.
		"an absolute path": {
			Material{Attach: []Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "/etc/ssl/cert.pem"}}},
			"repo-relative",
		},
		"a path escaping the repo": {
			Material{Attach: []Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "../elsewhere/cert.pem"}}},
			"repo-relative",
		},
		// The kernel appends material without deduplicating, so a repeated entry would double on
		// every record this cockpit ever writes.
		"the same evidence twice": {
			Material{Attach: []Attachment{
				{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "identity/signer.pem"},
				{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "identity/signer.pem"},
			}},
			"twice",
		},
		"a private key as a trust anchor": {
			Material{X509Roots: []string{"keys/ca.key"}},
			"not a private key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateMaterial(tc.in)
			if err == nil {
				t.Fatalf("expected a refusal mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say why (%q): %v", tc.want, err)
			}
		})
	}
}

// A root that is not a certificate would mean every attached certificate reports as carried
// forever, with nothing saying why. Caught at load, not at the first query that silently softens.
func TestCheckMaterialFiles_RefusesARootThatIsNotACertificate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "root.pem"), []byte("this is not a certificate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkMaterialFiles(dir, Material{X509Roots: []string{"root.pem"}})
	if err == nil || !strings.Contains(err.Error(), "no PEM certificate") {
		t.Fatalf("expected a refusal naming the unusable root, got %v", err)
	}
}

// Evidence that is not there is caught at load too: discovered mid-publish it would leave a record
// written and its evidence missing.
func TestCheckMaterialFiles_RefusesEvidenceThatIsNotThere(t *testing.T) {
	err := checkMaterialFiles(t.TempDir(), Material{Attach: []Attachment{{
		Scheme: "x509-cert", MediaType: "application/x-pem-file", File: "identity/signer.pem",
	}}})
	if err == nil || !strings.Contains(err.Error(), "identity/signer.pem") {
		t.Fatalf("expected a refusal naming the missing file, got %v", err)
	}
}

// X509Roots returns nil rather than the host's root store when none are configured. The verdict a
// reader gets must not depend on which machine ran the query.
func TestX509Roots_NoConfiguredRootsIsNilNotTheSystemPool(t *testing.T) {
	c := &Config{RepoRoot: t.TempDir()}
	if got := c.X509Roots(); got != nil {
		t.Fatal("an unconfigured repo must judge against no roots at all, not against the host's")
	}
}
