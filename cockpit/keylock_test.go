package cockpit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/keyload"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
	"kton.dev/plankton/core"
	"kton.dev/plankton/keys"
)

// ADR-021, the part the cockpit's core owns: an encrypted claim key signs with the right passphrase,
// is refused identity.locked without it or with a wrong one, and nothing is written when it is refused.

const testPass = "correct horse battery staple 8841"

// passphraseIs puts a passphrase where a CI run has it: the environment, which main takes into the
// process's hold. A test starts and ends with nothing held, so one test's value never answers another's.
func passphraseIs(t *testing.T, pass string) {
	t.Helper()
	keys.ForgetEnvPassphrase()
	t.Cleanup(keys.ForgetEnvPassphrase)
	os.Unsetenv(keys.EnvPassphrase)
	if pass != "" {
		t.Setenv(keys.EnvPassphrase, pass)
	}
}

// encryptInPlace turns the plain key at path into the encrypted form of the same seed.
func encryptInPlace(t *testing.T, path, pass string) {
	t.Helper()
	priv, err := keys.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := keys.MarshalEncrypted(priv, []byte(pass))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshot is a digest of everything the repository holds but its git directory, and its HEAD: what a
// refused say must leave as it found it (the registry, the blobs, the working tree, the history).
func snapshot(t *testing.T, r *testrepo.Repo) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(r.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if !d.IsDir() {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			sum := sha256.Sum256(b)
			lines = append(lines, p+" "+hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return r.HeadSHA(t) + "\n" + strings.Join(lines, "\n")
}

func workingOn(subject, step string) SayRequest {
	return SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": step, "by-session": testrepo.SessionID}}
}

func TestKeys_EncryptedClaimKeySignsWithTheRightPassphraseAndNothingElse(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	encryptInPlace(t, r.Key(testrepo.SessionID+"-claims"), testPass)
	c := New(Start{})
	ctx := context.Background()

	// A wrong passphrase: refused as identity.locked, naming the key and its keyid, and the repository
	// is byte for byte what it was, with no commit.
	before := snapshot(t, r)
	passphraseIs(t, "not the passphrase")
	_, err := c.Say(ctx, workingOn(subject, "analysis"))
	ref := refusalOf(t, err)
	if ref.Code != "identity.locked" || !strings.Contains(ref.Reason, "wrong passphrase") {
		t.Fatalf("a wrong passphrase: want identity.locked / wrong passphrase, got %s: %s", ref.Code, ref.Reason)
	}
	pub, perr := keyload.PublicKey(r.Key(testrepo.SessionID + "-claims"))
	if perr != nil {
		t.Fatal(perr)
	}
	if !strings.Contains(ref.Reason, r.Key(testrepo.SessionID+"-claims")) || !strings.Contains(ref.Reason, core.KeyIDHex(pub)) {
		t.Errorf("the refusal should name the key file and its keyid: %s", ref.Reason)
	}
	if strings.Contains(ref.Reason, "not the passphrase") {
		t.Errorf("a refusal must not echo the passphrase: %s", ref.Reason)
	}
	if after := snapshot(t, r); after != before {
		t.Errorf("a refused say changed the repository:\n--- before\n%s\n--- after\n%s", before, after)
	}

	// No passphrase at all (nobody to ask): refused the same way, saying how to unlock.
	passphraseIs(t, "")
	_, err = c.Say(ctx, workingOn(subject, "analysis"))
	ref = refusalOf(t, err)
	if ref.Code != "identity.locked" || !strings.Contains(ref.Reason, keys.EnvPassphrase) || !strings.Contains(ref.Reason, "terminal") {
		t.Errorf("no passphrase: want identity.locked naming the ways to unlock, got %s: %s", ref.Code, ref.Reason)
	}
	if after := snapshot(t, r); after != before {
		t.Error("a say without a passphrase changed the repository")
	}

	// The right one signs.
	passphraseIs(t, testPass)
	res, err := c.Say(ctx, workingOn(subject, "analysis"))
	if err != nil {
		t.Fatalf("say with the right passphrase: %v", err)
	}
	if !strings.HasPrefix(res.ClaimID, "sha256:") {
		t.Fatalf("no claim id: %+v", res)
	}
	if snapshot(t, r) == before {
		t.Error("a say with the right passphrase wrote nothing")
	}
}

// The encrypted form is the same key: its claims verify under the same keyid, in the same trust tier.
func TestKeys_PlainAndEncryptedFormsOfOneSeedSignUnderOneKeyID(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	c := New(Start{})
	ctx := context.Background()

	plain, err := c.Say(ctx, workingOn(subject, "plain"))
	if err != nil {
		t.Fatal(err)
	}
	encryptInPlace(t, r.Key(testrepo.SessionID+"-claims"), testPass)
	passphraseIs(t, testPass)
	enc, err := c.Say(ctx, workingOn(subject, "encrypted"))
	if err != nil {
		t.Fatal(err)
	}

	signer := map[string]string{}
	for _, rec := range feedAsk(t, AskRequest{}).Records {
		if rec.ID == plain.ClaimID || rec.ID == enc.ClaimID {
			if len(rec.Signers) != 1 {
				t.Fatalf("claim %s: want one trusted signer, got %+v", rec.ID, rec.Signers)
			}
			signer[rec.ID] = rec.Signers[0].KeyID + "/" + rec.Signers[0].Tier
		}
	}
	if len(signer) != 2 || signer[plain.ClaimID] != signer[enc.ClaimID] {
		t.Errorf("want both claims under one keyid and tier, got %v", signer)
	}
}

// The public half of an encrypted key is read without a passphrase: say --as checks a keyring key's
// half and its tier (CheckIdentity is that check, which a frontend runs when it opens) with nothing set.
func TestKeys_PublicHalfOfAnEncryptedKeyNeedsNoPassphrase(t *testing.T) {
	r := testrepo.New(t)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	encryptInPlace(t, filepath.Join(ring, "lane-a-claims.key"), testPass)
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	r.SetKeyring(t, ring)
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.WriteConfig(t, raw)
	r.Use(t)
	passphraseIs(t, "")

	cfg := r.Config(t)
	if err := CheckIdentity(cfg, "lane-a"); err != nil {
		t.Fatalf("an encrypted keyring key's public half should be readable without a passphrase: %v", err)
	}
	if _, _, err := keyOf(filepath.Join(ring, "lane-a-claims.key")); err != nil {
		t.Errorf("keyOf (the keyid of a key) asked for a passphrase: %v", err)
	}
}

// The MCP server has no one to ask: an encrypted key without KTON_KEY_PASSPHRASE is identity.locked,
// refused before anything is signed. The tool handler is the adapter the server registers.
func TestKeys_MCPRefusesAnEncryptedKeyBeforeSigning(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	encryptInPlace(t, r.Key(testrepo.SessionID+"-claims"), testPass)
	passphraseIs(t, "")
	before := snapshot(t, r)

	res, _, err := Say(context.Background(), nil, workingOn(subject, "analysis"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(errText(res), "encrypted") || !strings.Contains(errText(res), r.Key(testrepo.SessionID+"-claims")) {
		t.Errorf("want a tool error naming the encrypted key, got %v %q", res.IsError, errText(res))
	}
	if _, err := New(Start{}).Say(context.Background(), workingOn(subject, "analysis")); refusalOf(t, err).Code != "identity.locked" {
		t.Errorf("want identity.locked, got %v", err)
	}
	if snapshot(t, r) != before {
		t.Error("the refused say changed the repository")
	}
	var locked *keyload.LockedError
	if _, err := keyload.Load(r.Key(testrepo.SessionID + "-claims")); !errors.As(err, &locked) {
		t.Errorf("keyload.Load without a source: want a LockedError, got %v", err)
	}
}

// A batch asks once per key and signs all its claims or none: a wrong passphrase for the second
// claim's key refuses the whole batch before the first claim is stored.
func TestKeys_BatchWithAnEncryptedKeyAndAWrongPassphraseWritesNone(t *testing.T) {
	r := testrepo.New(t)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	encryptInPlace(t, filepath.Join(ring, "lane-a-claims.key"), testPass)
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	r.SetKeyring(t, ring)
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.WriteConfig(t, raw)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	c := New(Start{})
	ctx := context.Background()

	asLane := workingOn(subject, "two")
	asLane.As = "lane-a"
	batch := SayBatchRequest{Claims: []SayRequest{workingOn(subject, "one"), asLane, workingOn(subject, "three")}}

	before := snapshot(t, r)
	passphraseIs(t, "wrong")
	_, err := c.SayBatch(ctx, batch)
	if ref := refusalOf(t, err); ref.Code != "identity.locked" {
		t.Fatalf("a wrong passphrase: want identity.locked, got %s: %s", ref.Code, ref.Reason)
	}
	if snapshot(t, r) != before {
		t.Fatal("a refused batch wrote some of its claims")
	}

	passphraseIs(t, testPass)
	out, err := c.SayBatch(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Claims) != 3 {
		t.Fatalf("want three claims, got %d", len(out.Claims))
	}
}

// The passphrase is in no file the cockpit wrote, no trace and no answer, wrong or right.
func TestKeys_ThePassphraseIsWrittenNowhere(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	encryptInPlace(t, r.Key(testrepo.SessionID+"-claims"), testPass)
	t.Setenv("COCKPIT_TRACE", "1")
	var traced bytes.Buffer
	old := traceOut
	traceOut = &traced
	t.Cleanup(func() { traceOut = old })

	var said []string
	passphraseIs(t, "wrong"+testPass)
	_, err := New(Start{}).Say(context.Background(), workingOn(subject, "a"))
	said = append(said, err.Error())
	passphraseIs(t, testPass)
	res, err := New(Start{}).Say(context.Background(), workingOn(subject, "b"))
	if err != nil {
		t.Fatal(err)
	}
	said = append(said, res.ClaimID, res.Confirmation, traced.String())

	if traced.Len() == 0 {
		t.Fatal("the trace was empty: this test would prove nothing")
	}
	for _, s := range said {
		if strings.Contains(s, testPass) {
			t.Errorf("the passphrase is in an answer or trace: %q", s)
		}
	}
	for _, root := range []string{r.Root, r.Keys} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(testPass)) {
					t.Errorf("the passphrase is in %s", p)
				}
			}
			return nil
		})
	}
}

// The foton key is opened before publish touches anything: an encrypted one without its passphrase
// refuses with nothing committed, and with it the foton is signed under the same keyid as the plain form.
func TestKeys_PublishOpensTheEncryptedFotonKeyFirst(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")
	encryptInPlace(t, r.Key(testrepo.SessionID), testPass)
	req := PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "cp data/in.csv data/out.csv"}

	passphraseIs(t, "")
	head := r.HeadSHA(t)
	_, err := New(Start{}).Publish(context.Background(), req)
	if ref := refusalOf(t, err); ref.Code != "identity.locked" {
		t.Fatalf("want identity.locked, got %s: %s", ref.Code, ref.Reason)
	}
	if r.HeadSHA(t) != head {
		t.Error("a refused publish committed")
	}
	passphraseIs(t, testPass)
	if _, err := New(Start{}).Publish(context.Background(), req); err != nil {
		t.Fatalf("publish with the passphrase: %v", err)
	}
}

// A server takes no passphrase from anywhere, the right one in KTON_KEY_PASSPHRASE included: an unlock
// that held in the environment of the MCP server would make it a signing oracle for whatever reaches
// its connection (ADR-021 section 4). The tool handler refuses identity.locked and signs nothing.
func TestKeys_AServerRefusesAnEncryptedKeyEvenWithTheRightPassphraseInTheEnvironment(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	encryptInPlace(t, r.Key(testrepo.SessionID+"-claims"), testPass)
	keyload.SetServer(true)
	t.Cleanup(func() { keyload.SetServer(false) })
	passphraseIs(t, testPass) // set after the server started: it is still not read
	before := snapshot(t, r)

	res, _, err := Say(context.Background(), nil, workingOn(subject, "analysis"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(errText(res), "encrypted") {
		t.Errorf("want a tool error for the encrypted key, got %v %q", res.IsError, errText(res))
	}
	if _, err := New(Start{}).Say(context.Background(), workingOn(subject, "analysis")); refusalOf(t, err).Code != "identity.locked" {
		t.Errorf("want identity.locked, got %v", err)
	}
	if snapshot(t, r) != before {
		t.Error("a server signed or wrote with an encrypted key")
	}
}
