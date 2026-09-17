package anchor

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func pemOf(t *testing.T, pub any) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func cfgWith(a config.Anchor) *config.Config {
	c := &config.Config{}
	c.Raw.Anchor = a
	return c
}

// The pinned key is the whole trust root of an anchor: the SET is an ECDSA signature, and verifying
// it against a key the same endpoint just served is not verification at all. Inline PEM and a file
// path are both accepted because an operator has both — a key checked into the repo, or one pasted
// into the config.
func TestTrustedRekorPub_AcceptsAPinnedKeyInlineOrByPath(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	inline := pemOf(t, &key.PublicKey)
	path := filepath.Join(t.TempDir(), "rekor.pub")
	if err := os.WriteFile(path, []byte(inline), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, pin := range map[string]string{"inline PEM": inline, "a file path": path} {
		t.Run(name, func(t *testing.T) {
			got, err := trustedRekorPub(cfgWith(config.Anchor{Enabled: true, RekorPubkey: pin}))
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(&key.PublicKey) {
				t.Fatal("a different key came back than was pinned")
			}
		})
	}
}

// The case this exists for: a custom endpoint with nothing pinned. `internal/config` already
// refuses it at load, so this is the second of two — and it is the one that holds if the config
// ever grows a path that reaches here without that check.
func TestTrustedRekorPub_RefusesACustomLogWithNoPinnedKey(t *testing.T) {
	_, err := trustedRekorPub(cfgWith(config.Anchor{Enabled: true, RekorURL: "https://rekor.example.invalid"}))
	if err == nil {
		t.Fatal("a custom log with no pinned key was accepted")
	}
	if !strings.Contains(err.Error(), "self-verify") {
		t.Fatalf("the error does not explain the problem: %v", err)
	}
}

// Failing closed matters more here than elsewhere: a key that cannot verify a SET, accepted as if
// it could, is a witness that was never checked.
func TestTrustedRekorPub_RefusesAPinThatIsNotAnECDSAKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, pin := range map[string]string{
		"not PEM at all":   "sha256:not-a-key",
		"PEM, wrong curve": pemOf(t, pub),
		"PEM, empty block": "-----BEGIN PUBLIC KEY-----\n-----END PUBLIC KEY-----\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := trustedRekorPub(cfgWith(config.Anchor{Enabled: true, RekorPubkey: pin})); err == nil {
				t.Fatal("expected a refusal, got none")
			}
		})
	}
}

func TestTrimKeySuffix_FindsThePublicHalfBesideThePrivateOne(t *testing.T) {
	if got := trimKeySuffix("/repo/keys/session-1.key.pub"); got != "/repo/keys/session-1.pub" {
		t.Fatalf("got %q", got)
	}
}
