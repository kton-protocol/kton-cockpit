package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// peerCheckout is a peer's repository on disk publishing one public key, named as the peer named it.
func peerCheckout(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(dir, "registry", "keys"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "registry", "keys", name+".pub"), []byte(hex.EncodeToString(pub)+"\n"), 0o644))
	return dir
}

func readConfig(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "cockpit.config.json"))
	must(t, err)
	return string(b)
}

// Without --yes nothing is written; with it the key is copied in under the tier's name — never over
// this repository's own key of the same name — and the tier names it.
func TestTrustAdd_ShowsFirstThenWrites(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	peer := peerCheckout(t, testrepo.SessionID) // the peer calls theirs what this repo calls its own
	before := readConfig(t, r.Root)

	must(t, runTrust(context.Background(), []string{"add", "peer", peer}))
	if readConfig(t, r.Root) != before {
		t.Fatal("trust add without --yes changed the configuration")
	}

	must(t, runTrust(context.Background(), []string{"add", "peer", peer, "--yes"}))
	rel := "registry/keys/peer-" + testrepo.SessionID + ".pub"
	if _, err := os.Stat(filepath.Join(r.Root, rel)); err != nil {
		t.Fatalf("%s not written: %v", rel, err)
	}
	if !strings.Contains(readConfig(t, r.Root), rel) {
		t.Errorf("tier peer does not name %s:\n%s", rel, readConfig(t, r.Root))
	}
	own, _ := os.ReadFile(filepath.Join(r.Root, "registry", "keys", testrepo.SessionID+".pub"))
	theirs, _ := os.ReadFile(filepath.Join(peer, "registry", "keys", testrepo.SessionID+".pub"))
	if string(own) == string(theirs) {
		t.Error("the peer's key landed on this repository's own")
	}

	// The negative control: the same key under a second tier is not taken twice.
	must(t, runTrust(context.Background(), []string{"add", "other", peer, "--yes"}))
	if strings.Contains(readConfig(t, r.Root), `"other"`) {
		t.Errorf("a key already in tier peer was added to tier other")
	}
}

func TestTrust_SelfIsNotAPeerTier(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	if err := runTrust(context.Background(), []string{"add", "self", peerCheckout(t, "x"), "--yes"}); err == nil {
		t.Error("a peer was added to tier self")
	}
	if err := runTrust(context.Background(), []string{"remove", "self"}); err == nil {
		t.Error("tier self was removed")
	}
}
