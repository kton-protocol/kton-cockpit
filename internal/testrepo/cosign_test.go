package testrepo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// The same work, already a record here under somebody else's signature — an installed package's
// reference run, re-run by its user. With foton/v1 (kton §6.6.1) both producers sign the same bytes,
// the signatures union, and each keeps its own locators as material beside the record.
func TestAuthor_CoSignsWorkTheRegistryAlreadyHolds(t *testing.T) {
	r := New(t)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")
	cfg := r.Config(t)
	run := binaries.New(cfg)
	in := binaries.AuthorInput{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
		Cmd: "double data/in.csv > data/out.csv"}

	foreignKey, foreignPub := keyFile(t)
	theirs := in
	theirs.SignKey = foreignKey
	theirs.Located = []string{"data/out.csv=https://elsewhere.test/autorin/out.csv"}
	a, err := run.Author(context.Background(), theirs)
	if err != nil || a.CoSigned {
		t.Fatalf("the foreign record: %+v %v", a, err)
	}

	ours := in
	ours.SignKey = cfg.PlanktonKey
	ours.Located = []string{"data/out.csv=https://example.test/anwender/out.csv"}
	b, err := run.Author(context.Background(), ours)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || !b.CoSigned {
		t.Fatalf("the same work must co-sign the stored record: got %+v, stored %s", b, a.ID)
	}
	reg, err := pregistry.Open(cfg.PlanktonDir)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := reg.Envelope(a.ID)
	ourPub := pubOf(t, cfg.PlanktonKey)
	for name, pub := range map[string]ed25519.PublicKey{"foreign": foreignPub, "ours": ourPub} {
		if core.VerifiedSignerKeyID(env, []ed25519.PublicKey{pub}) == "" {
			t.Errorf("the stored record carries no valid %s signature (%d signatures)", name, len(env.Signatures))
		}
	}
	payload, _ := env.PayloadBytes()
	if strings.Contains(string(payload), "example.test") || strings.Contains(string(payload), "elsewhere.test") {
		t.Errorf("a foton/v1 record carries no locator in its signed bytes (kton §6.6.1)")
	}
	// Each producer's locators are its own statement beside the record.
	cfg.Raw.Trust.Tiers["fremd"] = []string{writePub(t, foreignPub)}
	locs, err := verify.Locators(context.Background(), run, cfg, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	where := map[string]string{}
	for _, l := range locs {
		where[l.Tier] = l.URI[0]
	}
	if where["fremd"] != "https://elsewhere.test/autorin/out.csv" || where["self"] != "https://example.test/anwender/out.csv" {
		t.Errorf("each producer keeps its own locators, got %v", where)
	}
}

// Publishing one's own work again is not a co-signature: the record already carries the signature.
func TestAuthor_RepublishingOwnWorkIsNotACoSignature(t *testing.T) {
	r := New(t)
	r.Write(t, "data/in.csv", "a\n")
	r.Write(t, "data/out.csv", "b\n")
	cfg := r.Config(t)
	in := binaries.AuthorInput{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
		Cmd: "copy", SignKey: cfg.PlanktonKey}
	for i, loc := range []string{"https://example.test/c1/out.csv", "https://example.test/c2/out.csv"} {
		in.Located = []string{"data/out.csv=" + loc}
		got, err := binaries.New(cfg).Author(context.Background(), in)
		if err != nil || got.CoSigned {
			t.Fatalf("publish %d: %+v %v", i+1, got, err)
		}
	}
}

func writePub(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fremd.pub")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(pub)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func keyFile(t *testing.T) (string, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "foreign.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(priv.Seed())), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, pub
}

func pubOf(t *testing.T, keyPath string) ed25519.PublicKey {
	t.Helper()
	b, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}
