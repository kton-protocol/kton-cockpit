package verify

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"kton.dev/plankton/core"
)

// This file carries forward a lesson from the process-wrapper era, where verification read a
// three-way exit code: a BROKEN TRUST CONFIGURATION must never read as "this signer is not
// trusted". A typo'd path in a tier once did exactly that — the key could not be read, the exit was
// non-zero, and the answer came back as a legitimate exclusion with no error surfaced.
//
// Linked, the shape is different but the trap is the same one: skipping a key that will not load
// silently narrows what the repository trusts, and every record that key signed then reports as
// untrusted. So an unusable key is an error, never a skip.

func writeKey(t *testing.T, dir, name string, pub ed25519.PublicKey) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(hex.EncodeToString(pub)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func cfgWithTier(root string, keys ...string) *config.Config {
	rel := make([]string, 0, len(keys))
	for _, k := range keys {
		rel = append(rel, filepath.Base(k))
	}
	return &config.Config{
		RepoRoot: root,
		Raw:      config.Raw{Trust: config.Trust{Tiers: map[string][]string{"self": rel}}},
	}
}

func TestTierKeys_IndexesEveryConfiguredKeyByItsKeyid(t *testing.T) {
	dir := t.TempDir()
	pubA, _, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	a := writeKey(t, dir, "a.pub", pubA)
	b := writeKey(t, dir, "b.pub", pubB)

	keys, byKeyID, err := tierKeys(cfgWithTier(dir, a, b))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || len(byKeyID) != 2 {
		t.Fatalf("expected both keys indexed, got %d keys and %d ids", len(keys), len(byKeyID))
	}
	if got := byKeyID[core.KeyIDHex(pubA)].tier; got != "self" {
		t.Errorf("key A resolved to tier %q, want self", got)
	}
}

// The case the old exit-code tests existed for, in its new form.
func TestTierKeys_AnUnusableKeyIsAnErrorNotASilentNarrowing(t *testing.T) {
	dir := t.TempDir()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	good := writeKey(t, dir, "good.pub", pub)

	t.Run("a path that does not exist", func(t *testing.T) {
		cfg := cfgWithTier(dir, good, filepath.Join(dir, "typo.pub"))
		_, _, err := tierKeys(cfg)
		if err == nil {
			t.Fatal("a trust tier naming an unreadable key was accepted; every record that key signed " +
				"would now report as untrusted, which is a working configuration's answer")
		}
		if !strings.Contains(err.Error(), "typo.pub") {
			t.Errorf("the error does not name the key that could not be read: %v", err)
		}
	})

	t.Run("a file that is not a key", func(t *testing.T) {
		bad := filepath.Join(dir, "notakey.pub")
		if err := os.WriteFile(bad, []byte("-----BEGIN SOMETHING-----\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := tierKeys(cfgWithTier(dir, good, bad))
		if err == nil {
			t.Fatal("a trust tier naming a file that is not a public key was accepted")
		}
		if !strings.Contains(err.Error(), "not a public key") {
			t.Errorf("the error does not say what is wrong with it: %v", err)
		}
	})
}

// And the verdict itself: a record signed by a key outside the configuration is UNTRUSTED with no
// error — not forged, just outside this repo's trust. The two must stay distinguishable.
func TestVerifiedSignerKeyID_AStrangerIsUntrustedNotAnError(t *testing.T) {
	dir := t.TempDir()
	pubMine, privMine, _ := ed25519.GenerateKey(rand.Reader)
	pubTheirs, privTheirs, _ := ed25519.GenerateKey(rand.Reader)
	writeKey(t, dir, "mine.pub", pubMine)

	keys, byKeyID, err := tierKeys(cfgWithTier(dir, filepath.Join(dir, "mine.pub")))
	if err != nil {
		t.Fatal(err)
	}

	mine := signed(t, privMine, pubMine)
	if keyid := core.VerifiedSignerKeyID(mine, keys); byKeyID[keyid].tier != "self" {
		t.Fatalf("a record signed by a configured key did not resolve to its tier (keyid %q)", keyid)
	}
	theirs := signed(t, privTheirs, pubTheirs)
	if keyid := core.VerifiedSignerKeyID(theirs, keys); keyid != "" {
		t.Fatalf("a record signed by a key in no tier resolved to keyid %q", keyid)
	}
}

// signed builds a minimal DSSE envelope the kernel will verify, so the test exercises the kernel's
// own verification rather than a stand-in for it.
func signed(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey) core.Envelope {
	t.Helper()
	payload := []byte(`{"_type":"https://in-toto.io/Statement/v1"}`)
	env := core.Envelope{PayloadType: core.PayloadType}
	env.Payload = base64.StdEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, core.PAE(core.PayloadType, payload))
	// Written as an anonymous struct literal because that is how core.Envelope declares the field —
	// there is no named signature type to refer to, so a caller constructing an envelope has to
	// repeat its shape.
	env.Signatures = append(env.Signatures, struct {
		KeyID string `json:"keyid"`
		Sig   string `json:"sig"`
	}{KeyID: core.KeyIDHex(pub), Sig: base64.StdEncoding.EncodeToString(sig)})
	return env
}
