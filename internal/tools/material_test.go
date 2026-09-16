package tools

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// These cover the whole carried-evidence path against the real kernel: a certificate issued for the
// key the fixture actually signs with is attached by publish, stored by plankton, read back, and
// reported by ask as verified — with every step performed by the code a session would use.
//
// The certificate is MINTED HERE from the repo's own generated key rather than checked in. A
// fixture certificate would expire, and a suite that then reported carried instead of verified
// would look like a passing suite with one softer answer.

// certifyRepoKey issues a throwaway root and a leaf certificate for this repo's plankton signing
// key, writes both into the repo, and returns their repo-relative paths.
func certifyRepoKey(t *testing.T, r *testrepo.Repo, commonName string, notAfter time.Time) (certPath, rootPath string) {
	t.Helper()
	pubHex, err := os.ReadFile(filepath.Join(r.Root, "registry/keys", testrepo.SessionID+".pub"))
	if err != nil {
		t.Fatalf("reading the fixture's signing pubkey: %v", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		t.Fatalf("the fixture's pubkey is not 32 bytes of hex: %v (%d bytes)", err, len(raw))
	}
	return certifyKey(t, r, ed25519.PublicKey(raw), commonName, notAfter)
}

// certifyKey is the same, for any key — so a test can issue a valid certificate that belongs to
// somebody else.
func certifyKey(t *testing.T, r *testrepo.Repo, subjectKey ed25519.PublicKey, commonName string, notAfter time.Time) (certPath, rootPath string) {
	t.Helper()
	caPub, caKey, _ := ed25519.GenerateKey(rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Cockpit Test Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, caPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, subjectKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	certPath = "identity/signer.pem"
	rootPath = "identity/root.pem"
	r.Write(t, certPath, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})))
	r.Write(t, rootPath, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})))
	return certPath, rootPath
}

// TestMaterial_ACertificateForTheSigningKeyIsAttachedAndReportedVerified is the end-to-end claim:
// configured evidence rides on a published record, and ask says who the signing key belongs to.
func TestMaterial_ACertificateForTheSigningKeyIsAttachedAndReportedVerified(t *testing.T) {
	r := testrepo.New(t)
	certPath, rootPath := certifyRepoKey(t, r, "Jane Researcher", time.Now().Add(time.Hour))
	raw := testrepo.DefaultConfig()
	raw.Material = config.Material{
		Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		X509Roots: []string{rootPath},
	}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	rec := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID)

	if len(rec.Material) != 1 {
		t.Fatalf("expected exactly the one configured attachment on the record, got %d: %+v", len(rec.Material), rec.Material)
	}
	m := rec.Material[0]
	if m.Verdict != material.Verified {
		t.Fatalf("want %q, got %q (%s)", material.Verified, m.Verdict, m.Detail)
	}
	if !strings.Contains(m.Detail, "Jane Researcher") {
		t.Errorf("ask does not report WHO the certificate identifies: %q", m.Detail)
	}
	if m.Scheme != "x509-cert" || m.MediaType != "application/x-pem-file" {
		t.Errorf("the scheme/media type the config declared did not survive the round trip: %+v", m)
	}
}

// TestMaterial_AnUnknownSchemeIsCarriedNotRejected is the rule the kernel states and this config
// inherits: evidence nothing here can read is attached anyway, and reported as carried. A cockpit
// that refused it would turn its own scheme list into a protocol version.
func TestMaterial_AnUnknownSchemeIsCarriedNotRejected(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "identity/house-badge.bin", "\x01\x02\x03 not a format anyone here knows")
	raw := testrepo.DefaultConfig()
	raw.Material = config.Material{Attach: []config.Attachment{{
		Scheme: "acme-internal-badge-v3", MediaType: "application/octet-stream", File: "identity/house-badge.bin",
	}}}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	rec := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID)

	if len(rec.Material) != 1 {
		t.Fatalf("the unlisted scheme was not attached at all: %+v", rec.Material)
	}
	if got := rec.Material[0].Verdict; got != material.Carried {
		t.Fatalf("want %q for evidence nothing can read, got %q (%s)", material.Carried, got, rec.Material[0].Detail)
	}
	if rec.Material[0].Scheme != "acme-internal-badge-v3" {
		t.Errorf("the scheme token was not preserved: %+v", rec.Material[0])
	}
}

// TestMaterial_ACertificateForSomebodyElseIsReportedFailed: the binding check, end to end. A valid
// certificate that belongs to another key must not let a record borrow that identity.
func TestMaterial_ACertificateForSomebodyElseIsReportedFailed(t *testing.T) {
	r := testrepo.New(t)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	certPath, rootPath := certifyKey(t, r, otherPub, "Somebody Else", time.Now().Add(time.Hour))
	raw := testrepo.DefaultConfig()
	raw.Material = config.Material{
		Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		X509Roots: []string{rootPath},
	}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	rec := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID)

	if len(rec.Material) != 1 {
		t.Fatalf("expected one attachment, got %d", len(rec.Material))
	}
	if got := rec.Material[0].Verdict; got != material.Failed {
		t.Fatalf("want %q for a certificate belonging to another key, got %q (%s)", material.Failed, got, rec.Material[0].Detail)
	}
}

// TestMaterial_NoConfiguredMaterialMeansNoneReported: the default is unchanged. A repo that
// configures nothing publishes records that carry nothing and report nothing.
func TestMaterial_NoConfiguredMaterialMeansNoneReported(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	pub := publishOne(t, r)
	rec := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID)
	if len(rec.Material) != 0 {
		t.Fatalf("a repo that configured no material reported some: %+v", rec.Material)
	}
}

// TestMaterial_ClaimsCarryItToo: `say` attaches the same evidence, so who VOUCHED for a result is
// attributable exactly as who produced it is.
func TestMaterial_ClaimsCarryItToo(t *testing.T) {
	r := testrepo.New(t)
	pubHex, err := os.ReadFile(filepath.Join(r.Root, "registry/keys", testrepo.SessionID+"-claims.pub"))
	if err != nil {
		t.Fatal(err)
	}
	raw32, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
	if err != nil || len(raw32) != ed25519.PublicKeySize {
		t.Fatalf("the fixture's claim pubkey is not 32 bytes of hex: %v", err)
	}
	certPath, rootPath := certifyKey(t, r, ed25519.PublicKey(raw32), "Jane Researcher", time.Now().Add(time.Hour))

	cfgRaw := testrepo.DefaultConfig()
	cfgRaw.Material = config.Material{
		Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		X509Roots: []string{rootPath},
	}
	r.WriteConfig(t, cfgRaw)
	r.Use(t)

	pub := publishOne(t, r)
	sayResult, sayOut, err := Say(context.Background(), nil, SayInput{
		Subject:  pub.FotonID,
		Template: "working-on",
		Fields:   map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("Say returned a Go error: %v", err)
	}
	if sayResult.IsError {
		t.Fatalf("say failed: %+v", errText(sayResult))
	}

	askResult, askOut, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if askResult.IsError {
		t.Fatalf("ask failed: %+v", errText(askResult))
	}
	for _, rec := range askOut.Records {
		if rec.ID != sayOut.ClaimID {
			continue
		}
		if len(rec.Material) != 1 || rec.Material[0].Verdict != material.Verified {
			t.Fatalf("the claim does not carry the configured certificate as verified: %+v", rec.Material)
		}
		return
	}
	t.Fatalf("ask did not return the claim %s at all: %+v", sayOut.ClaimID, askOut.Records)
}

// askFotonRecord asks for the producer of a hash and returns the RecordVerification for the foton
// under test, so each case asserts against what a session would actually be handed.
func askFotonRecord(t *testing.T, outputHash, fotonID string) RecordVerification {
	t.Helper()
	if outputHash == "" {
		t.Fatal("publish reported no hash for the output; there is nothing to ask about")
	}
	result, out, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: outputHash})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ask failed: %+v", errText(result))
	}
	for _, rec := range out.Records {
		if rec.ID == fotonID {
			if !rec.Verified {
				t.Fatalf("the freshly published foton did not verify against this repo's own trust tier: %+v", rec)
			}
			return rec
		}
	}
	t.Fatalf("ask did not return the published foton %s: %+v", fotonID, out.Records)
	return RecordVerification{}
}

// TestMaterial_EvidenceOnAnExcludedRecordIsNotReported: evidence attached to a record the caller
// was not given is content from that record, and routing it out through a material list would walk
// around the filter that excluded it — the same hole Raw's redaction closes for scraped lines.
func TestMaterial_EvidenceOnAnExcludedRecordIsNotReported(t *testing.T) {
	r := testrepo.New(t)
	certPath, rootPath := certifyRepoKey(t, r, "Jane Researcher", time.Now().Add(time.Hour))

	// A second tier holding a key that signed nothing here, so a filter naming it is valid — the
	// validator refuses an unknown tier — and excludes every record this repo wrote.
	strangerPub := make([]byte, ed25519.PublicKeySize)
	if _, err := rand.Read(strangerPub); err != nil {
		t.Fatal(err)
	}
	r.Write(t, "registry/keys/stranger.pub", hex.EncodeToString(strangerPub)+"\n")

	raw := testrepo.DefaultConfig()
	raw.Material = config.Material{
		Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		X509Roots: []string{rootPath},
	}
	raw.Trust.Tiers["partners"] = []string{"registry/keys/stranger.pub"}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)

	// Without the filter the evidence is there — otherwise this test could pass on a bug that
	// attached nothing at all.
	if got := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID); len(got.Material) != 1 {
		t.Fatalf("control: the record should carry its certificate unfiltered, got %+v", got.Material)
	}

	result, out, err := Ask(context.Background(), nil, AskInput{
		Query:  "producer",
		Ref:    pub.OutputHashes["data/out.csv"],
		Filter: &AskFilter{TrustTier: "partners"},
	})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ask failed: %+v", errText(result))
	}
	for _, rec := range out.Records {
		if rec.ID != pub.FotonID {
			continue
		}
		if len(rec.Material) != 0 {
			t.Fatalf("the excluded record still reported its evidence: %+v", rec.Material)
		}
		return
	}
	// Not being listed at all is also correct — what must not happen is being listed WITH evidence.
}

// TestMaterial_PreflightReportsTheVerdictBeforeAnythingIsPublished: the doctor path. A certificate
// for the wrong key, or one with no root to judge it against, otherwise produces valid records that
// report CARRIED forever while the operator believes an identity was established.
func TestMaterial_PreflightReportsTheVerdictBeforeAnythingIsPublished(t *testing.T) {
	t.Run("a certificate for the signing key with a configured root verifies", func(t *testing.T) {
		r := testrepo.New(t)
		certPath, rootPath := certifyRepoKey(t, r, "Jane Researcher", time.Now().Add(time.Hour))
		raw := testrepo.DefaultConfig()
		raw.Material = config.Material{
			Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
			X509Roots: []string{rootPath},
		}
		r.WriteConfig(t, raw)

		got := material.Preflight(r.Config(t))
		if len(got) != 1 || got[0].Verdict != material.Verified {
			t.Fatalf("want one %q report, got %+v", material.Verified, got)
		}
	})

	t.Run("a certificate with no configured root is carried, and says why", func(t *testing.T) {
		r := testrepo.New(t)
		certPath, _ := certifyRepoKey(t, r, "Jane Researcher", time.Now().Add(time.Hour))
		raw := testrepo.DefaultConfig()
		raw.Material = config.Material{
			Attach: []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		}
		r.WriteConfig(t, raw)

		got := material.Preflight(r.Config(t))
		if len(got) != 1 || got[0].Verdict != material.Carried {
			t.Fatalf("want one %q report, got %+v", material.Carried, got)
		}
		if !strings.Contains(got[0].Detail, "x509Roots") {
			t.Errorf("the operator is not told what to configure: %q", got[0].Detail)
		}
	})

	t.Run("a certificate for the claim key is not reported as belonging to nobody", func(t *testing.T) {
		// Preflight tries both signing keys. A certificate issued for the nekton key must not report
		// FAILED merely because the plankton key was tried first.
		r := testrepo.New(t)
		pubHex, err := os.ReadFile(filepath.Join(r.Root, "keys", testrepo.SessionID+"-claims.pub"))
		if err != nil {
			t.Fatal(err)
		}
		raw32, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
		if err != nil {
			t.Fatal(err)
		}
		certPath, rootPath := certifyKey(t, r, ed25519.PublicKey(raw32), "Jane Researcher", time.Now().Add(time.Hour))
		cfgRaw := testrepo.DefaultConfig()
		cfgRaw.Material = config.Material{
			Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
			X509Roots: []string{rootPath},
		}
		r.WriteConfig(t, cfgRaw)

		got := material.Preflight(r.Config(t))
		if len(got) != 1 || got[0].Verdict != material.Verified {
			t.Fatalf("want one %q report for the claim key's certificate, got %+v", material.Verified, got)
		}
	})
}

// TestMaterial_TheVerdictIsNeverStored is the mechanical half of "the verdict does not federate".
//
// Operators assume the opposite — "verified" reads like a property of the record, and it is a
// property of the reading — so it is worth checking rather than asserting in prose. What is stored
// is the configured bytes and nothing else: no verdict, no reason, no subject line. A peer that
// mirrors this record receives the evidence and re-evaluates it against ITS OWN configured roots,
// and may legitimately reach a different answer about identical bytes.
func TestMaterial_TheVerdictIsNeverStored(t *testing.T) {
	r := testrepo.New(t)
	certPath, rootPath := certifyRepoKey(t, r, "Jane Researcher", time.Now().Add(time.Hour))
	raw := testrepo.DefaultConfig()
	raw.Material = config.Material{
		Attach:    []config.Attachment{{Scheme: "x509-cert", MediaType: "application/x-pem-file", File: certPath}},
		X509Roots: []string{rootPath},
	}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)

	// The control: this repo does report it as verified, so the assertion below is about what
	// TRAVELS rather than about a verdict that was never reached.
	if got := askFotonRecord(t, pub.OutputHashes["data/out.csv"], pub.FotonID); len(got.Material) != 1 ||
		got.Material[0].Verdict != material.Verified {
		t.Fatalf("control: this repo should report the certificate verified, got %+v", got.Material)
	}

	cmd := exec.Command(r.Root+"/bin/plankton", "material", pub.FotonID, "--json")
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "PLANKTON_DIR="+filepath.Join(r.Root, "registry/plankton"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("plankton material: %v", err)
	}
	var read struct {
		Material []struct {
			Scheme   string `json:"scheme"`
			Material string `json:"material"`
		} `json:"material"`
	}
	if err := json.Unmarshal(out, &read); err != nil {
		t.Fatalf("could not read the stored material: %v\n%s", err, out)
	}
	if len(read.Material) != 1 {
		t.Fatalf("expected exactly the one attachment, got %d", len(read.Material))
	}
	stored, err := base64.StdEncoding.DecodeString(read.Material[0].Material)
	if err != nil {
		t.Fatalf("the stored material is not readable: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(r.Root, certPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, onDisk) {
		t.Fatalf("what was stored is not byte-for-byte the configured file (%d vs %d bytes)", len(stored), len(onDisk))
	}
	// Named explicitly, so a future change that helpfully records the verdict alongside the evidence
	// fails here instead of quietly exporting this repo's trust decisions to every peer.
	for _, leak := range []string{"verdict", "verified", "checkedBy", "Jane Researcher"} {
		if bytes.Contains(out, []byte(`"`+leak+`"`)) && leak != "verified" {
			t.Errorf("the stored record carries %q; a verdict must not travel with the evidence", leak)
		}
	}
}

// TestAsk_RawCarriesOnlyIncludedRecords guards the property that replaced line redaction.
//
// `Raw` used to be produced by scraping the kernel's text output and then blanking the lines of
// records that had been excluded — which only worked under an assumption nothing guaranteed, that a
// record's id is the first hash on its line. kton #57 made the reads structured, and the mechanism
// is now stronger: an excluded record is never assembled into the answer in the first place.
//
// The all-excluded case is covered by TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier.
// This is the MIXED case, which is where a regression would hide: both claims verify, one is
// filtered out, and the answer must carry exactly one of them. A build that went back to
// concatenating everything would still pass the all-excluded test, because there the answer is
// empty either way.
func TestAsk_RawCarriesOnlyIncludedRecords(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)

	// Two claims about the same subject, signed by the same key, both verifying. They differ only in
	// whether they carry a reproduction level, which is what the filter below selects on.
	r.Write(t, "data/rerun.csv", "id,result\n1,84\n") // byte-identical to data/out.csv
	repResult, repOut, err := Say(context.Background(), nil, SayInput{
		Subject:           pub.FotonID,
		Template:          "reproduces",
		SubjectOutputHash: pub.OutputHashes["data/out.csv"],
		ReproducedOutput:  "data/rerun.csv",
		ReproducedFotonID: pub.FotonID,
	})
	if err != nil || repResult.IsError {
		t.Fatalf("say reproduces: err=%v %s", err, errText(repResult))
	}
	workingID := sayWorkingOn(t, pub.FotonID)

	result, out, err := Ask(context.Background(), nil, AskInput{
		Query: "about", Ref: pub.FotonID,
		Filter: &AskFilter{Level: "L0"},
	})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("about query failed: %+v", errText(result))
	}

	// The control: the filter must actually have kept one and dropped the other, or the assertion
	// below is about an empty answer and proves nothing.
	if len(out.Included) != 1 || out.Included[0] != repOut.ClaimID {
		t.Fatalf("control: expected exactly the reproduces claim included, got %+v", out.Included)
	}
	if len(out.Excluded) != 1 || out.Excluded[0] != workingID {
		t.Fatalf("control: expected exactly the working-on claim excluded, got %+v", out.Excluded)
	}

	if !strings.Contains(out.Raw, repOut.ClaimID) {
		t.Errorf("the included claim is missing from Raw, so this asserts nothing about what leaked:\n%s", out.Raw)
	}
	if strings.Contains(out.Raw, workingID) {
		t.Errorf("an excluded record's content reached Raw:\n%s", out.Raw)
	}
	for _, c := range out.Claims {
		if c.ID == workingID {
			t.Errorf("an excluded record was assembled into Claims: %+v", c)
		}
	}
}
