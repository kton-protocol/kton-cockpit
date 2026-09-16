// Package anchor witnesses a signed record in a Sigstore Rekor transparency log and stores the
// proof beside it.
//
// The kernel assigns this here in as many words: `kton anchor`'s own source says it "is not part of
// the kernel — plankton records fotons and never needs the network — it is a cockpit-invoked
// capability", and that the verified Rekor coordinates "are meant to be stored as a sidecar on the
// claim/foton". So this package drives `kton.dev/kton/sigstore` and attaches what comes back; it
// implements no transparency-log logic of its own. Every cryptographic step below — the submission,
// the Signed Entry Timestamp, the inclusion proof, the binding — is a call into that package.
//
// What an anchor adds, precisely: a signature says WHO signed, not WHEN, and a signer who later
// produces a different record can claim that one was the original. Rekor attests that this exact
// record existed by a given time, in an append-only log a third party can audit. This does not
// merely submit — it verifies the inclusion proof and the Signed Entry Timestamp, and then verifies
// that the entry BINDS to this envelope and this verifier, so a hostile endpoint replaying a real
// but unrelated entry is rejected rather than reported as anchored.
package anchor

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kton.dev/kton/sigstore"
	"kton.dev/plankton/core"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// Kind selects which substrate holds the record, which decides both the pubkey to anchor under and
// the registry the resulting evidence is attached to.
type Kind int

const (
	Foton Kind = iota
	Claim
)

// Entry is what Rekor answered, as far as this cockpit reports it. The full entry is stored beside
// the record; these are the coordinates worth handing back to a caller.
type Entry struct {
	LogIndex int64  `json:"logIndex"`
	UUID     string `json:"uuid"`
}

// Submitter is the network half of anchoring, in one seam: submit the envelope and hand back an
// entry that has already passed every check below.
//
// It is a seam because anchoring writes to a public, permanent log. The kernel gates its own live
// Rekor test behind a `live` build tag for that reason, and a suite that anchored on every run
// would leave a trail of entries nobody can withdraw — so the default suite replaces this one
// function and exercises everything on this side of it: the envelope found by id, the verifier
// derived from the key that actually signed, the entry stored as kton §8.1 material and committed with
// the record. What it does NOT cover is that Rekor behaves as expected. That is the live test's
// job, and this paragraph is here so nobody mistakes a green run for one.
type Submitter func(ctx context.Context, cfg *config.Config, env core.Envelope, verifierPEM []byte) (*sigstore.Entry, error)

var submit Submitter = live

// Use replaces the submitter and returns a function that puts the real one back. Only tests call
// it; production has exactly one submitter and no way to configure another.
func Use(s Submitter) (restore func()) {
	prev := submit
	submit = s
	return func() { submit = prev }
}

// Record anchors one record and attaches the verified entry to it as verification material.
//
// The evidence is stored under the `rekor-entry` scheme of kton §8.1 — external evidence ABOUT a
// record, which the kernel keeps as opaque bytes and never evaluates, exactly as it stores a DSSE
// signature without checking it on ingest. Verification is this cockpit's business, and it already
// happened: every check below refuses rather than reports.
func Record(ctx context.Context, cfg *config.Config, recordID string, kind Kind) (*Entry, error) {
	r := binaries.New(cfg)
	raw, err := r.EnvelopeFor(ctx, recordID)
	if err != nil {
		return nil, err
	}
	var env core.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("reading the envelope of %s: %w", recordID, err)
	}

	// The signing key's own public half: the anchor is submitted under the identity that signed the
	// record, so Rekor's entry binds to that verifier and not to some other key this repo holds.
	keyPath := cfg.PlanktonKey + ".pub"
	if kind == Claim {
		keyPath = cfg.NektonKey + ".pub"
	}
	hexKey, err := os.ReadFile(trimKeySuffix(keyPath))
	if err != nil {
		return nil, err
	}
	pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(hexKey)))
	if err != nil {
		return nil, err
	}
	verifierPEM, err := sigstore.Ed25519VerifierPEM(pub)
	if err != nil {
		return nil, err
	}

	entry, err := submit(ctx, cfg, env, verifierPEM)
	if err != nil {
		return nil, fmt.Errorf("anchoring %s in Rekor failed: %w", recordID, err)
	}
	if entry == nil || entry.UUID == "" {
		return nil, fmt.Errorf("Rekor returned an entry with no uuid for %s", recordID)
	}
	entryJSON, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "cockpit-anchor-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	entryPath := filepath.Join(dir, "rekor-entry.json")
	if err := os.WriteFile(entryPath, entryJSON, 0o600); err != nil {
		return nil, err
	}
	attach := r.AttachFoton
	if kind == Claim {
		attach = r.AttachClaim
	}
	if err := attach(ctx, recordID, "rekor-entry", "application/json", entryPath); err != nil {
		return nil, fmt.Errorf("the record was anchored but attaching the proof failed: %w", err)
	}
	return &Entry{LogIndex: entry.LogIndex, UUID: entry.UUID}, nil
}

// live is the real submitter: POST to Rekor, then refuse unless all three checks pass.
func live(ctx context.Context, cfg *config.Config, env core.Envelope, verifierPEM []byte) (*sigstore.Entry, error) {
	url := cfg.Raw.Anchor.RekorURL
	entry, err := sigstore.Anchor(url, env, verifierPEM)
	if err != nil {
		return nil, err
	}
	rekorPub, err := trustedRekorPub(cfg)
	if err != nil {
		return nil, err
	}
	if err := entry.VerifySET(rekorPub); err != nil {
		return nil, fmt.Errorf("Rekor SET did not verify: %w", err)
	}
	if err := entry.VerifyInclusion(); err != nil {
		return nil, fmt.Errorf("Rekor inclusion proof did not verify: %w", err)
	}
	// The SET + inclusion proof only say "this is a genuine Rekor entry" — NOT "this is YOUR
	// record". A hostile endpoint can replay a real, unrelated entry; binding the entry to the
	// submitted envelope and verifier is what rejects that.
	if err := entry.VerifyBinds(env, verifierPEM); err != nil {
		return nil, fmt.Errorf("Rekor entry is not bound to this record: %w", err)
	}
	return entry, nil
}

// trustedRekorPub returns the key the SET is verified against, and it must be a PINNED one — never
// the key the endpoint serves for itself, or a fabricated entry from an attacker-controlled
// `rekor_url` would self-verify: its own SET, signed by its own key, checked against that same key.
//
// So: a pinned key from the config is always used; a custom endpoint with no pinned key is REFUSED;
// only the well-known public Rekor falls back to the fetched key, and then with the caveat said out
// loud. This is the kernel's own policy, applied from the config rather than from the environment —
// which is the line this cockpit draws (CLAUDE.md, "What not to add here").
func trustedRekorPub(cfg *config.Config) (*ecdsa.PublicKey, error) {
	if pin := cfg.Raw.Anchor.RekorPubkey; pin != "" {
		txt := pin
		if b, err := os.ReadFile(pin); err == nil {
			txt = string(b)
		}
		blk, _ := pem.Decode([]byte(txt))
		if blk == nil {
			return nil, fmt.Errorf("anchor.rekor_pubkey: not a PEM public key")
		}
		parsed, err := x509.ParsePKIXPublicKey(blk.Bytes)
		if err != nil {
			return nil, err
		}
		ec, ok := parsed.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("anchor.rekor_pubkey: not an ECDSA key")
		}
		return ec, nil
	}
	if cfg.Raw.Anchor.RekorURL != "" {
		return nil, fmt.Errorf("a custom Rekor endpoint (%s) must be paired with a pinned public key "+
			"in anchor.rekor_pubkey; refusing to trust the key the endpoint serves for itself "+
			"(a fabricated entry would otherwise self-verify)", cfg.Raw.Anchor.RekorURL)
	}
	fmt.Fprintln(os.Stderr, "warning: verifying against the key the PUBLIC Rekor endpoint serves for "+
		"itself (UNPINNED, trust-on-first-use); set anchor.rekor_pubkey to a pinned key for a real trust root.")
	return sigstore.PublicKey("")
}

// trimKeySuffix turns keys/x.key.pub into keys/x.pub — the config names the private half, and the
// public half sits beside it under the same stem.
func trimKeySuffix(p string) string {
	return filepath.Join(filepath.Dir(p),
		strings.TrimSuffix(filepath.Base(p), ".key.pub")+".pub")
}
