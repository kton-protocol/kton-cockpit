package material

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
)

// mkCert issues a certificate for pub, signed by the given issuer (self-signed when issuerCert is
// nil), so a test can build a real chain rather than assert against a fixture nobody can regenerate.
func mkCert(t *testing.T, cn string, pub ed25519.PublicKey, issuerCert *x509.Certificate, issuerKey ed25519.PrivateKey, notAfter time.Time) (*x509.Certificate, []byte) {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	parent, signWith := tmpl, issuerKey
	if issuerCert != nil {
		parent = issuerCert
	} else {
		tmpl.IsCA = true
		tmpl.BasicConstraintsValid = true
		tmpl.KeyUsage |= x509.KeyUsageCertSign
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signWith)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return c, der
}

func stored(scheme string, raw []byte) binaries.StoredMaterial {
	return binaries.StoredMaterial{
		Scheme: scheme, MediaType: "application/pkix-cert",
		Material: base64.StdEncoding.EncodeToString(raw),
	}
}

// TestEvaluate_CertificateBoundToSigningKeyAndChainingToAConfiguredRoot is the whole point of the
// certificate path: identity is reported only when the certificate is BOTH about the key that
// actually signed the record AND issued by a root this repo declared.
func TestEvaluate_CertificateBoundToSigningKeyAndChainingToAConfiguredRoot(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))

	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, leafDER := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(time.Hour))

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	got := evaluate(stored("x509-cert", leafDER), signerPub, roots)
	if got.Verdict != Verified {
		t.Fatalf("want %q, got %q (%s)", Verified, got.Verdict, got.Detail)
	}
	if got.CheckedBy == "" {
		t.Error("a verified verdict must name what produced it; checkedBy is empty")
	}
	if !strings.Contains(got.Detail, "Jane Researcher") {
		t.Errorf("the verdict does not say WHO the certificate identifies: %q", got.Detail)
	}
}

// TestEvaluate_CertificateForAnotherKeyIsFailed: a perfectly valid certificate that belongs to some
// other key proves nothing about this record. Reporting it as verified would let any record borrow
// any identity, which is the "declared, not verified" hole in a new place.
func TestEvaluate_CertificateForAnotherKeyIsFailed(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, leafDER := mkCert(t, "Somebody Else", otherPub, caCert, caKey, time.Now().Add(time.Hour))

	signerPub, _, _ := ed25519.GenerateKey(rand.Reader) // the key that signed the record
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	got := evaluate(stored("x509-cert", leafDER), signerPub, roots)
	if got.Verdict != Failed {
		t.Fatalf("a certificate for a different key must be %q, got %q (%s)", Failed, got.Verdict, got.Detail)
	}
	// The reason separates this from damaged bytes: this one is a file wired to the wrong record and
	// is fixed by editing the config, not by investigating an attack.
	if got.Reason != NotBound {
		t.Errorf("want reason %q, got %q — a caller cannot tell a misconfiguration from bitrot", NotBound, got.Reason)
	}
}

// TestEvaluate_ExpiredCertificateIsFailed: the chain check is crypto/x509's, and the validity
// window is part of it. Asserted so an option change that disables time checking is caught.
func TestEvaluate_ExpiredCertificateIsFailed(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))

	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, leafDER := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(-time.Minute))

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	got := evaluate(stored("x509-cert", leafDER), signerPub, roots)
	if got.Verdict != Failed {
		t.Fatalf("an expired certificate must be %q, got %q (%s)", Failed, got.Verdict, got.Detail)
	}
	if got.Reason != Chain {
		t.Errorf("want reason %q for a certificate nothing configured vouches for, got %q", Chain, got.Reason)
	}
}

// TestEvaluate_NoConfiguredRootsIsCarriedNotVerified: with no trust anchor declared, nothing was
// judged. Falling back to the host root store would make the answer depend on the machine.
func TestEvaluate_NoConfiguredRootsIsCarriedNotVerified(t *testing.T) {
	signerPub, signerKey, _ := ed25519.GenerateKey(rand.Reader)
	_, der := mkCert(t, "Jane Researcher", signerPub, nil, signerKey, time.Now().Add(time.Hour))

	got := evaluate(stored("x509-cert", der), signerPub, nil)
	if got.Verdict != Carried {
		t.Fatalf("want %q with no configured roots, got %q (%s)", Carried, got.Verdict, got.Detail)
	}
	if !strings.Contains(got.Detail, "x509Roots") {
		t.Errorf("the detail must say WHY nothing was checked: %q", got.Detail)
	}
}

// TestEvaluate_PEMAndDERAreBothAccepted: the package refuses no spelling of a certificate, because
// prescribing one is exactly what it does not do.
func TestEvaluate_PEMAndDERAreBothAccepted(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, der := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(time.Hour))
	asPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	for name, raw := range map[string][]byte{"DER": der, "PEM": asPEM} {
		if got := evaluate(stored("x509-cert", raw), signerPub, roots); got.Verdict != Verified {
			t.Errorf("%s: want %q, got %q (%s)", name, Verified, got.Verdict, got.Detail)
		}
	}
}

// TestEvaluate_RecognisesTheArtifactNotTheSchemeToken: a certificate filed under a house-specific
// scheme name is still checked. This is the "accept any key we can process" rule, mechanically.
func TestEvaluate_RecognisesTheArtifactNotTheSchemeToken(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, der := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(time.Hour))

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	got := evaluate(stored("acme-internal-badge-v3", der), signerPub, roots)
	if got.Verdict != Verified {
		t.Fatalf("an unlisted scheme carrying a real certificate must still be checked; got %q (%s)", got.Verdict, got.Detail)
	}
}

// TestEvaluate_UnreadableEvidenceIsCarriedAndSaysSo: the honest middle answer. Not verified, not
// failed — carried, with a reason, so a reader never mistakes silence for absence.
func TestEvaluate_UnreadableEvidenceIsCarriedAndSaysSo(t *testing.T) {
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	got := evaluate(stored("jades", []byte(`{"protected":"eyJhbGciOiJFUzI1NiJ9"}`)), signerPub, nil)
	if got.Verdict != Carried {
		t.Fatalf("want %q, got %q", Carried, got.Verdict)
	}
	if !strings.Contains(got.Detail, "jades") {
		t.Errorf("the detail must name the scheme nothing could read: %q", got.Detail)
	}
	if got.CheckedBy != "" {
		t.Errorf("nothing checked it, so checkedBy must be empty; got %q", got.CheckedBy)
	}
}

// TestEvaluate_RekorEntrySaysItWasVerifiedWhenAttached: the one scheme this cockpit writes itself.
// A bare "carried" would understate it — `kton anchor` verified it before it was ever stored.
func TestEvaluate_RekorEntrySaysItWasVerifiedWhenAttached(t *testing.T) {
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	got := evaluate(stored("rekor-entry", []byte(`{"logIndex":1,"uuid":"abc"}`)), signerPub, nil)
	if got.Verdict != Carried {
		t.Fatalf("want %q (it is not re-checked on read), got %q", Carried, got.Verdict)
	}
	if !strings.Contains(got.Detail, "kton anchor") {
		t.Errorf("the detail must say it WAS verified when attached: %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "not re-checked") {
		t.Errorf("the detail must also say it is not re-checked now: %q", got.Detail)
	}
}

// TestEvaluate_CorruptBase64IsFailed: kton §8.1 guarantees material never invalidates a record, and it
// does not — but evidence that cannot even be decoded is unusable, and saying nothing about it
// would let a reader assume it was fine.
func TestEvaluate_CorruptBase64IsFailed(t *testing.T) {
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	got := evaluate(binaries.StoredMaterial{Scheme: "rfc3161", Material: "not!base64!"}, signerPub, nil)
	if got.Verdict != Failed {
		t.Fatalf("want %q for undecodable material, got %q", Failed, got.Verdict)
	}
	if got.Reason != Unreadable {
		t.Errorf("want reason %q for damaged bytes, got %q", Unreadable, got.Reason)
	}
}

// TestEvaluate_EveryFailureNamesWhichOne: the property behind the individual cases. A failed verdict
// with no reason forces a caller back into parsing Detail, which is keying on a sentence — the
// mistake this project removed from say.go's reproduction level.
func TestEvaluate_EveryFailureNamesWhichOne(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	_, wrongKey := mkCert(t, "Somebody Else", otherPub, caCert, caKey, time.Now().Add(time.Hour))
	_, expired := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(-time.Minute))

	cases := map[string]binaries.StoredMaterial{
		"a certificate for another key": stored("x509-cert", wrongKey),
		"an expired certificate":        stored("x509-cert", expired),
		"undecodable bytes":             {Scheme: "rfc3161", Material: "not!base64!"},
	}
	for name, in := range cases {
		got := evaluate(in, signerPub, roots)
		if got.Verdict != Failed {
			t.Fatalf("%s: expected a failure to assert a reason about, got %q", name, got.Verdict)
		}
		if got.Reason == "" {
			t.Errorf("%s: failed with no reason — a caller can only tell why by reading prose", name)
		}
	}
}

// And the inverse: a verdict that is not a failure must not carry a reason, or "reason" starts
// meaning two things depending on which verdict it sits beside.
func TestEvaluate_OnlyFailuresCarryAReason(t *testing.T) {
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caCert, _ := mkCert(t, "Test Root", caPub, nil, caKey, time.Now().Add(24*time.Hour))
	signerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, der := mkCert(t, "Jane Researcher", signerPub, caCert, caKey, time.Now().Add(time.Hour))
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	for name, got := range map[string]Report{
		"verified": evaluate(stored("x509-cert", der), signerPub, roots),
		"carried":  evaluate(stored("jades", []byte(`{"protected":"eyJ9"}`)), signerPub, roots),
	} {
		if got.Reason != "" {
			t.Errorf("%s carries reason %q; reason belongs to a failure only", name, got.Reason)
		}
	}
}

// TestEvaluate_NoVerifyingKeyMeansNoBinding: when the record verified against no configured key
// there is nothing to bind a certificate to, and the report must say that rather than imply the
// certificate was judged.
func TestEvaluate_NoVerifyingKeyMeansNoBinding(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	_, der := mkCert(t, "Jane Researcher", pub, nil, key, time.Now().Add(time.Hour))

	got := evaluate(stored("x509-cert", der), nil, x509.NewCertPool())
	if got.Verdict != Carried {
		t.Fatalf("want %q, got %q (%s)", Carried, got.Verdict, got.Detail)
	}
	if !strings.Contains(got.Detail, "not bound") {
		t.Errorf("the detail must say the certificate was not bound to anything: %q", got.Detail)
	}
}
