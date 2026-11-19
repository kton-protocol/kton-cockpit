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
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// peerCheckout is a peer's repository on disk publishing one public key, named as the peer named it.
func peerCheckout(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(dir, ".kton", "keys", "self"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".kton", "keys", "self", name+".pub"), []byte(hex.EncodeToString(pub)+"\n"), 0o644))
	return dir
}

// tierFiles lists the public keys in a tier's folder by file name.
func tierFiles(t *testing.T, root, tier string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(root, ".kton", "keys", tier, "*.pub"))
	var out []string
	for _, p := range m {
		out = append(out, filepath.Base(p))
	}
	return out
}

func readConfig(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(config.ConfigPath(root))
	must(t, err)
	return string(b)
}

// Without --yes nothing is written; with it the key is copied into the tier's folder — never over
// this repository's own key of the same name — and the configuration names the tier.
func TestTrustAdd_ShowsFirstThenWrites(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)
	peer := peerCheckout(t, testrepo.SessionID) // the peer calls theirs what this repo calls its own
	before := readConfig(t, r.Root)

	must(t, runTrust(context.Background(), []string{"add", "peer", peer}))
	if readConfig(t, r.Root) != before || len(tierFiles(t, r.Root, "peer")) != 0 {
		t.Fatal("trust add without --yes changed something")
	}

	must(t, runTrust(context.Background(), []string{"add", "peer", peer, "--yes"}))
	if got := tierFiles(t, r.Root, "peer"); len(got) != 1 || got[0] != testrepo.SessionID+".pub" {
		t.Fatalf("tier peer holds %v", got)
	}
	if !strings.Contains(readConfig(t, r.Root), `"peer"`) {
		t.Errorf("the configuration does not name tier peer:\n%s", readConfig(t, r.Root))
	}
	own, _ := os.ReadFile(filepath.Join(r.Root, ".kton", "keys", "self", testrepo.SessionID+".pub"))
	theirs, _ := os.ReadFile(filepath.Join(peer, ".kton", "keys", "self", testrepo.SessionID+".pub"))
	if string(own) == string(theirs) {
		t.Error("the peer's key landed on this repository's own")
	}

	// The negative control: the same key under a second tier is not taken twice.
	must(t, runTrust(context.Background(), []string{"add", "other", peer, "--yes"}))
	if strings.Contains(readConfig(t, r.Root), `"other"`) || len(tierFiles(t, r.Root, "other")) != 0 {
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

// A peer publishes the keys it trusts beside its own, in its other tiers' folders. Only its own —
// its tier self — are the peer; a third party in its tier "partners" is not taken under its name.
func TestTrustAdd_TakesThePeersOwnKeysOnly(t *testing.T) {
	peer := testrepo.New(t)
	third := peerCheckout(t, "third")
	b, _ := os.ReadFile(filepath.Join(third, ".kton", "keys", "self", "third.pub"))
	must(t, os.MkdirAll(filepath.Join(peer.Root, ".kton", "keys", "partners"), 0o755))
	must(t, os.WriteFile(filepath.Join(peer.Root, ".kton", "keys", "partners", "third.pub"), b, 0o644))

	r := testrepo.New(t)
	t.Chdir(r.Root)
	must(t, runTrust(context.Background(), []string{"add", "peer", peer.Root, "--yes"}))
	got := strings.Join(tierFiles(t, r.Root, "peer"), " ")
	if !strings.Contains(got, testrepo.SessionID+".pub") {
		t.Errorf("the peer's own key was not taken: %s", got)
	}
	if strings.Contains(got, "third") {
		t.Errorf("a key the peer only trusts was taken as the peer: %s", got)
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
	if got := tierFiles(t, r.Root, "peer"); len(got) != 1 || got[0] != "remote-peer.pub" {
		t.Errorf("the key the git repository publishes was not taken: %v", got)
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
	t.Chdir(filepath.Join(r.Root, "data")) // a relative source path is the configuration's, not the shell's
	must(t, runTrust(context.Background(), []string{"add", "peer", "hub", "--yes"}))
	if got := tierFiles(t, r.Root, "peer"); len(got) != 1 || got[0] != "hub-peer.pub" {
		t.Errorf("the key the configured source publishes was not taken: %v", got)
	}
}
