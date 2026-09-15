// Package material carries external evidence about a record, and evaluates as much of it as this
// repo is actually able to evaluate.
//
// kton §8.1 makes verification material deliberately open: the kernel stores opaque bytes under a
// scheme token it never interprets, and `plankton attach` refuses nothing for being unfamiliar,
// because "refusing unknown evidence would make this list a protocol version". This package keeps
// that stance and adds the half the kernel leaves to a cockpit — deciding what any of it is worth.
//
// It holds to three rules, and the third is the one that makes the other two safe:
//
//  1. Carry anything. A scheme nobody here has heard of is attached exactly like a familiar one.
//  2. Check whatever can be checked, by what the bytes ARE rather than by what the scheme token
//     claims. A certificate is recognised by parsing as a certificate; if it parses, it is checked,
//     whatever it was filed under.
//  3. Say which of the two happened, per item. CARRIED and VERIFIED are different answers and are
//     never collapsed: counting unevaluated evidence as verified overstates, and dropping it
//     understates, and a reader cannot tell either from a record that simply says nothing.
//
// It adds no cryptography of its own. The chain check is crypto/x509's, against roots this repo's
// config names — never the host's root store, which would make the verdict depend on which machine
// ran the query.
package material

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// Kind selects which substrate holds the record, and so which binary stores and returns its
// evidence. It mirrors anchor.Kind and verify.Kind rather than sharing one, because a single
// shared enum would make every one of those packages import whichever package defined it.
type Kind int

const (
	Foton Kind = iota
	Claim
)

// Verdict is what this cockpit is prepared to say about one piece of evidence.
type Verdict string

const (
	// Verified: this cockpit checked the evidence and it holds.
	Verified Verdict = "verified"
	// Carried: the evidence travels with the record and nothing here evaluated it. Not a
	// suspicion and not a defect — most evidence is for a consumer other than this one.
	Carried Verdict = "carried"
	// Failed: this cockpit checked the evidence and it does NOT hold. Reported, never silently
	// dropped and never used to exclude the record on its own: whose word counts is a trust
	// decision, and trust here is `cockpit.config.json` plus a reader, not this package.
	Failed Verdict = "failed"
)

// Reason says WHICH failure, and exists because "failed" alone collapses two findings that demand
// opposite responses: bytes that are damaged or forged, and a perfectly good certificate wired to
// the wrong record. Reading that difference out of the prose in Detail would be keying on a
// sentence, which is the mistake this project removed from say.go's reproduction level.
//
// It is a separate axis from Verdict on purpose. Verdict answers "did we check it and did it hold",
// and for all three of these the answer is the same no; a fourth verdict would make that one axis
// mean two things.
type Reason string

const (
	// NotBound: the certificate parses and may be entirely valid, but it belongs to a different key
	// than the one that verified this record. Almost always a misconfigured file — the operator
	// pointed material.attach at the wrong certificate — and almost never an attack, since a forged
	// binding is what the check makes impossible rather than what it detects.
	NotBound Reason = "not-bound"
	// Unreadable: the bytes cannot be decoded or parsed as what they are filed as. Damage or
	// tampering, not configuration. Worth investigating rather than editing.
	Unreadable Reason = "unreadable"
	// Chain: bound to the right key, but nothing this repository configured vouches for it —
	// expired, or issued by an unknown root. Renew it, or add the root.
	Chain Reason = "chain"
)

// Report is one piece of attached evidence, as this cockpit describes it.
type Report struct {
	Scheme    string  `json:"scheme"`
	MediaType string  `json:"mediaType"`
	Bytes     int     `json:"bytes"`
	Verdict   Verdict `json:"verdict"`
	// Reason is set only on a Failed verdict and names which failure it is, so a caller can act on
	// it without parsing Detail. See Reason.
	Reason Reason `json:"reason,omitempty"`
	// CheckedBy names what produced the verdict, so "verified" is never an unattributed claim. It is
	// a fixed string written in this package, never a value from the configuration — a
	// configurable one would be a self-asserted label, which is what kton §7.2 says about `by`.
	CheckedBy string `json:"checkedBy,omitempty"`
	// Detail says what was found — the certificate's subject, or why nothing could be checked.
	Detail string `json:"detail,omitempty"`
}

// A Report is produced HERE, for THIS query, and is never written anywhere. Nothing in this package
// stores a verdict: Attach writes the configured bytes and only those, so what a peer receives when
// a record is mirrored is the evidence, never the judgement.
//
// That is the correct behaviour and it is also the one operators assume backwards. A verdict is a
// statement about what THIS repository's configuration trusts — which roots it named, which keys are
// in its tiers — and carrying it to a peer would be asserting that repository's conclusions inside
// another one's trust boundary. A peer re-evaluates with its own roots and may legitimately reach a
// different answer about the identical bytes. Said out loud here because "verified" reads like a
// property of the record, and it is a property of the reading.

// Attach records every attachment this repo's config declares onto one record.
//
// Attaching is all-or-nothing on the first failure, and the error says the record already exists:
// material binds to a content address, so a record whose evidence failed to attach is complete and
// valid — it simply carries less than intended, and a caller must be able to tell that from a
// record that was never written.
func Attach(ctx context.Context, cfg *config.Config, r *binaries.Runner, recordID string, kind Kind) error {
	for _, a := range cfg.Attachments() {
		var err error
		switch kind {
		case Claim:
			err = r.AttachClaim(ctx, recordID, a.Scheme, a.MediaType, a.File)
		default:
			err = r.AttachFoton(ctx, recordID, a.Scheme, a.MediaType, a.File)
		}
		if err != nil {
			return fmt.Errorf("record %s is registered, but attaching %s evidence from %s failed: %w",
				recordID, a.Scheme, a.File, err)
		}
	}
	return nil
}

// Describe reads back everything attached to a record and evaluates what it can.
//
// verifyingKeyPath is the pubkey that ACTUALLY verified this record (verify.ResolveTier's second
// return), not a declared keyid. It is what an attached certificate is checked to bind to: a
// certificate that names a person but belongs to some other key says nothing about this record, and
// binding to a declared identity would reintroduce exactly the "declared, not verified" hole the
// verify package exists to close. An empty path means the record verified against no configured
// key, and then no certificate can be bound and every item reports as carried.
func Describe(ctx context.Context, cfg *config.Config, r *binaries.Runner, recordID string, kind Kind, verifyingKeyPath string) ([]Report, error) {
	var stored []binaries.StoredMaterial
	var err error
	switch kind {
	case Claim:
		stored, err = r.MaterialForClaim(ctx, recordID)
	default:
		stored, err = r.MaterialForFoton(ctx, recordID)
	}
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return nil, nil
	}

	signer := signingKey(verifyingKeyPath)
	roots := cfg.X509Roots()

	out := make([]Report, 0, len(stored))
	for _, s := range stored {
		out = append(out, evaluate(s, signer, roots))
	}
	return out, nil
}

// evaluate decides one item's verdict from the BYTES, not from the scheme token.
//
// The order matters: try to recognise the artifact first, and fall back to the token only for a
// note. A certificate filed under a house-specific scheme name is still a certificate and is still
// checked; a scheme this cockpit knows the name of but cannot parse is still only carried.
func evaluate(s binaries.StoredMaterial, signer ed25519.PublicKey, roots *x509.CertPool) Report {
	rep := Report{Scheme: s.Scheme, MediaType: s.MediaType, Verdict: Carried}

	raw, err := base64.StdEncoding.DecodeString(s.Material)
	if err != nil {
		// Stored material that will not even base64-decode is corrupt, and saying so is the point:
		// kton §8.1 guarantees material never affects a record's validity, so the record stands — but the
		// evidence is unusable and a reader must not read silence as absence.
		rep.Verdict = Failed
		rep.Reason = Unreadable
		rep.CheckedBy = "base64 decode"
		rep.Detail = "the stored bytes are not valid base64, so this evidence cannot be read at all"
		return rep
	}
	rep.Bytes = len(raw)

	cert, ok := parseCertificate(raw)
	if !ok {
		rep.Detail = carriedNote(s.Scheme)
		return rep
	}
	return checkCertificate(rep, cert, signer, roots)
}

// parseCertificate accepts a certificate however it was stored — PEM or bare DER. Refusing one
// spelling would be prescribing a form, which is the thing this package does not do.
func parseCertificate(raw []byte) (*x509.Certificate, bool) {
	rest := raw
	for {
		blk, next := pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			if c, err := x509.ParseCertificate(blk.Bytes); err == nil {
				return c, true
			}
		}
		rest = next
	}
	if c, err := x509.ParseCertificate(raw); err == nil {
		return c, true
	}
	return nil, false
}

// checkCertificate answers two independent questions and reports the weaker of the two answers.
//
//  1. Does this certificate belong to the key that actually signed the record? Without that, the
//     certificate is about someone else and proves nothing here, however impeccable its chain.
//  2. Does it chain to a root THIS repo configured, within its validity window?
//
// Both must hold for VERIFIED. A binding failure is FAILED — the evidence was checked and is not
// about this record. No configured root is CARRIED, not failed: nothing was checked, and a repo
// that has declared no trust anchor has not made a judgement to report.
func checkCertificate(rep Report, cert *x509.Certificate, signer ed25519.PublicKey, roots *x509.CertPool) Report {
	subject := cert.Subject.String()

	if signer == nil {
		rep.Detail = fmt.Sprintf("certificate for %q; not bound, because this record verified against no configured trust-tier key", subject)
		return rep
	}
	certKey, isEd := cert.PublicKey.(ed25519.PublicKey)
	if !isEd || !certKey.Equal(signer) {
		rep.Verdict = Failed
		rep.Reason = NotBound
		rep.CheckedBy = "key binding"
		rep.Detail = fmt.Sprintf(
			"certificate for %q does not belong to the key that signed this record — it may be a valid "+
				"certificate for someone else, but it says nothing about this record", subject)
		return rep
	}

	if roots == nil {
		rep.CheckedBy = "key binding"
		rep.Detail = fmt.Sprintf(
			"certificate for %q belongs to the signing key; its chain was NOT checked, because this repo "+
				"configures no material.x509Roots to judge it against", subject)
		return rep
	}

	chains, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		rep.Verdict = Failed
		rep.Reason = Chain
		rep.CheckedBy = "crypto/x509 chain verify against material.x509Roots"
		rep.Detail = fmt.Sprintf("certificate for %q belongs to the signing key but does not verify: %v", subject, err)
		return rep
	}

	rep.Verdict = Verified
	rep.CheckedBy = "crypto/x509 chain verify against material.x509Roots, plus key binding"
	rep.Detail = fmt.Sprintf("%s, issued by %s, valid until %s (%d chain(s))",
		subject, cert.Issuer.String(), cert.NotAfter.UTC().Format(time.RFC3339), len(chains))
	return rep
}

// carriedNote says why an item was not evaluated, and for the one scheme this cockpit writes
// itself, when it WAS evaluated even though it is not being re-checked now.
//
// That distinction is not cosmetic: `kton anchor` verifies the inclusion proof, the Signed Entry
// Timestamp and the entry's binding to this envelope BEFORE the proof is ever attached, and refuses
// rather than prints on any of the three. So a stored rekor-entry was verified once, at write time.
// It is not re-verified on read because no kernel command re-verifies a stored entry, and doing the
// ECDSA check here would be this cockpit growing its own transparency-log cryptography. Reported
// this way, a reader learns both facts instead of guessing from a bare "carried".
func carriedNote(scheme string) string {
	if scheme == "rekor-entry" {
		return "a Rekor entry, verified by `kton anchor` (inclusion proof, SET, and binding to this " +
			"record) at the moment it was attached; not re-checked on read — kton §8.1: presence is " +
			"not a check"
	}
	return fmt.Sprintf("carried as-is: nothing here can evaluate %q evidence, and it is attached so a consumer that can, may", scheme)
}

// signingKey reads a plankton/nekton pubkey file, which holds the 32-byte Ed25519 key as hex.
// Returns nil for an empty path or anything unreadable: nil means "no binding can be checked",
// which callers already report as carried.
func signingKey(path string) ed25519.PublicKey {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}

// Preflight evaluates the attachments a repository has configured, against that repository's own
// signing keys — the same evaluation Describe performs on a real record, run before any record
// exists.
//
// It exists because the failure it catches is silent otherwise. A certificate issued for the wrong
// key, or a repository that attached a certificate and configured no root to judge it against,
// produces records that are perfectly valid and report CARRIED forever. Nothing fails, and the
// identity the operator thought they had established is simply never asserted. `cockpit doctor` is
// where that should surface, not a query months later.
//
// Both signing keys are tried, because a certificate is normally issued for one of them and the
// other will legitimately not match: the strongest answer across the two is the one reported.
func Preflight(cfg *config.Config) []Report {
	signers := []ed25519.PublicKey{
		signingKey(pubHalf(cfg.PlanktonKey)),
		signingKey(pubHalf(cfg.NektonKey)),
	}
	roots := cfg.X509Roots()

	out := make([]Report, 0, len(cfg.Raw.Material.Attach))
	for _, a := range cfg.Attachments() {
		b, err := os.ReadFile(a.File)
		if err != nil {
			out = append(out, Report{
				Scheme: a.Scheme, MediaType: a.MediaType, Verdict: Failed, Reason: Unreadable,
				CheckedBy: "reading the configured file",
				Detail:    fmt.Sprintf("%s cannot be read: %v", a.File, err),
			})
			continue
		}
		s := binaries.StoredMaterial{
			Scheme: a.Scheme, MediaType: a.MediaType,
			Material: base64.StdEncoding.EncodeToString(b),
		}
		out = append(out, best(s, signers, roots))
	}
	return out
}

// best reports the strongest verdict any of this repo's signing keys produces. A certificate issued
// for the plankton key does not belong to the nekton key, and reporting that as FAILED because the
// other key was tried second would be an artefact of iteration order rather than a finding.
func best(s binaries.StoredMaterial, signers []ed25519.PublicKey, roots *x509.CertPool) Report {
	rank := map[Verdict]int{Failed: 0, Carried: 1, Verified: 2}
	var top Report
	for i, signer := range signers {
		got := evaluate(s, signer, roots)
		if i == 0 || rank[got.Verdict] > rank[top.Verdict] {
			top = got
		}
	}
	return top
}

// pubHalf turns the configured private key path into its public half: keygen writes `x.key` and
// `x.pub` side by side.
func pubHalf(privateKeyPath string) string {
	return strings.TrimSuffix(privateKeyPath, ".key") + ".pub"
}
