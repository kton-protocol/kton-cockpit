package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
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

// A peer publishes the keys it trusts beside its own. Only its own — its tier self — are the peer;
// the others were taken into the peer's tier, so a third party counted under the peer's name.
func TestTrustAdd_TakesThePeersOwnKeysOnly(t *testing.T) {
	peer := testrepo.New(t)
	third := peerCheckout(t, "third")
	b, _ := os.ReadFile(filepath.Join(third, "registry", "keys", "third.pub"))
	must(t, os.WriteFile(filepath.Join(peer.Root, "registry", "keys", "third.pub"), b, 0o644))

	r := testrepo.New(t)
	t.Chdir(r.Root)
	must(t, runTrust(context.Background(), []string{"add", "peer", peer.Root, "--yes"}))
	cfg := readConfig(t, r.Root)
	if !strings.Contains(cfg, "peer-"+testrepo.SessionID+".pub") {
		t.Errorf("the peer's own key was not taken:\n%s", cfg)
	}
	if strings.Contains(cfg, "third") {
		t.Errorf("a key the peer only trusts was taken as the peer:\n%s", cfg)
	}
}

// A peer given as a git repository rather than a checkout is read through the git source kind, at the
// revision it is at — no git call of the CLI's own.
func TestTrustAdd_ReadsAPeerFromAGitRepository(t *testing.T) {
	work := peerCheckout(t, "remote-peer")
	gitIn := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn(work, "init", "--quiet", "-b", "main")
	gitIn(work, "add", ".")
	gitIn(work, "-c", "user.name=p", "-c", "user.email=p@x", "commit", "--quiet", "-m", "keys")
	bare := filepath.Join(t.TempDir(), "peer.git")
	gitIn("", "clone", "--quiet", "--bare", work, bare)

	r := testrepo.New(t)
	t.Chdir(r.Root)
	must(t, runTrust(context.Background(), []string{"add", "peer", bare, "--yes"}))
	if cfg := readConfig(t, r.Root); !strings.Contains(cfg, "remote-peer.pub") {
		t.Errorf("the key the git repository publishes was not taken:\n%s", cfg)
	}
}

// A configured federation source is a peer by its name, read through its own kind — so a kind an
// extension brings (improve) reaches trust add too.
func TestTrustAdd_ReadsAPeerFromAConfiguredSourceByName(t *testing.T) {
	peer := peerCheckout(t, "hub-peer")
	r := testrepo.New(t)
	rel, err := filepath.Rel(r.Root, peer)
	must(t, err)
	raw := testrepo.DefaultConfig()
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "hub", "kind": "dir", "path": rel})
	must(t, json.Unmarshal(b, &spec))
	raw.Federation.Sources = []source.Spec{spec}
	r.WriteConfig(t, raw)
	t.Chdir(filepath.Join(r.Root, "registry")) // a relative source path is the configuration's, not the shell's
	must(t, runTrust(context.Background(), []string{"add", "peer", "hub", "--yes"}))
	if cfg := readConfig(t, r.Root); !strings.Contains(cfg, "hub-peer.pub") {
		t.Errorf("the key the configured source publishes was not taken:\n%s", cfg)
	}
}
