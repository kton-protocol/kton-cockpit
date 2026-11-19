package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// doctor --json names every claim key the checkout can sign as: its own and each key of the
// keyring, with the key id from the seed, whether the .pub beside it matches, and its tier
// (userinterface's #151 follow-up: a window with a person's and an agent's key).
func TestDoctorJSON_ListsEveryClaimKey(t *testing.T) {
	r := testrepo.New(t)
	ring := t.TempDir()
	key := func(name string, pubOf ed25519.PublicKey) string {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		os.WriteFile(filepath.Join(ring, name+"-claims.key"), []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600)
		if pubOf != nil {
			pub = pubOf
		}
		os.WriteFile(filepath.Join(ring, name+"-claims.pub"), []byte(hex.EncodeToString(pub)+"\n"), 0o644)
		return hex.EncodeToString(pub)
	}
	agent := key("agent", nil)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	key("broken", other) // a .pub that is not this key's half
	r.Write(t, ".kton/keys/agents/agent-claims.pub", agent+"\n")
	raw := testrepo.DefaultConfig()
	raw.Trust.Tiers = append(raw.Trust.Tiers, "agents")
	r.SetKeyring(t, ring)
	r.WriteConfig(t, raw)

	stdout, stderr, _ := r.CockpitSplit(t, "doctor", "--json")
	var rep struct {
		Fields struct {
			Identity struct {
				Keys []struct {
					Name, KeyID, Tier string
					Matches           bool
				} `json:"keys"`
			} `json:"identity"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("doctor --json: %v\n%s\n%s", err, stdout, stderr)
	}
	got := map[string]struct {
		tier    string
		matches bool
		id      bool
	}{}
	for _, k := range rep.Fields.Identity.Keys {
		got[k.Name] = struct {
			tier    string
			matches bool
			id      bool
		}{k.Tier, k.Matches, k.KeyID != ""}
	}
	if k := got[testrepo.SessionID]; !k.id || !k.matches || k.tier != "self" {
		t.Errorf("the checkout's own key: %+v", k)
	}
	if k := got["agent"]; !k.id || !k.matches || k.tier != "agents" {
		t.Errorf("the keyring's agent key: %+v", k)
	}
	if k := got["broken"]; !k.id || k.matches || k.tier != "" {
		t.Errorf("a key whose .pub does not match: %+v", k)
	}
	if len(rep.Fields.Identity.Keys) != 3 {
		t.Errorf("want 3 keys, got %+v", rep.Fields.Identity.Keys)
	}
}
