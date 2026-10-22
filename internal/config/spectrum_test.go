package config

import "testing"

// An environment class is a content hash in canonical form, as `plankton author --environment`
// requires: anything looser would still be a class to the record, and with the image carried beside
// it (ADR-010) two images would share a record under a class that names nothing.
func TestConfig_SpectrumIsACanonicalContentHash(t *testing.T) {
	good := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, s := range []string{"sha256:", "sha256:abcd", "SHA256:" + good[7:], "sha256:" + "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef", good[7:], " " + good} {
		if ValidateSpectrum(s) == nil {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{"", good} {
		if err := ValidateSpectrum(s); err != nil {
			t.Errorf("refused %q: %v", s, err)
		}
	}
}
