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
	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"
)

// The same work, already a record here under somebody else's signature — an installed package's
// reference run, re-run by its user. The ids agree, the carried locators do not (they pin each
// repo's own commit), and the kernel merges no signatures across differing bytes. Adding our own
// envelope would be answered "already there" with our signature gone, and publish used to report
// that as success. The cockpit must sign the STORED payload instead, and say that it did.
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
	if !strings.Contains(string(payload), "elsewhere.test/autorin") || strings.Contains(string(payload), "anwender") {
		t.Errorf("the stored record's own locators must be kept and ours not slipped under its signatures")
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
