package cockpit

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

// keyringKey writes <name>-claims.key and .pub into ring and returns the public half in hex.
func keyringKey(t *testing.T, ring, name string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	os.WriteFile(filepath.Join(ring, name+"-claims.key"), []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600)
	os.WriteFile(filepath.Join(ring, name+"-claims.pub"), []byte(pubHex+"\n"), 0o644)
	return pubHex
}

// A request names who it acts as: say --as signs with that key of the keyring, and the claim is
// attributed to it, in its own tier. Only a key a trust tier holds can be named; a repository
// without a keyring signs with its own key only (SPEC §8.7).
func TestSay_AsAnotherIdentitySignsWithThatKey(t *testing.T) {
	r := testrepo.New(t)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	keyringKey(t, ring, "stranger")
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	r.SetKeyring(t, ring)
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.WriteConfig(t, raw)
	r.Use(t)
	subject := publishOne(t, r).FotonID

	say := func(as string) (string, string) {
		res, out, err := Say(context.Background(), nil, SayRequest{Subject: subject, Template: "working-on", As: as,
			Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			return "", errText(res)
		}
		return out.ClaimID, ""
	}
	id, why := say("lane-a")
	if why != "" {
		t.Fatalf("say --as lane-a: %s", why)
	}
	p := feedAsk(t, AskRequest{})
	var found bool
	for _, rec := range p.Records {
		if rec.ID == id {
			found = true
			if len(rec.Signers) != 1 || rec.Signers[0].Tier != "lanes" {
				t.Errorf("want the claim signed by lane-a, in tier lanes: %+v", rec.Signers)
			}
		}
	}
	if !found {
		t.Fatalf("the claim %s is not among the trusted records", id)
	}

	for as, want := range map[string]string{
		"stranger": "no trust tier",
		"missing":  "no key missing-claims.key",
		"../x":     "not a key name",
	} {
		if _, why := say(as); !strings.Contains(why, want) {
			t.Errorf("say --as %s: want refused with %q, got %q", as, want, why)
		}
	}
	os.WriteFile(filepath.Join(ring, "stranger-claims.pub"), []byte(lane+"\n"), 0o644)
	if _, why := say("stranger"); !strings.Contains(why, "not the public half") {
		t.Errorf("a .pub that is not the key's: want refused, got %q", why)
	}

	r.SetKeyring(t, "")
	r.WriteConfig(t, raw)
	if _, why := say("lane-a"); !strings.Contains(why, "names no keyring") {
		t.Errorf("no keyring: want refused, got %q", why)
	}
}

// by-session names who speaks, and the signature already says it. Every form of say fills it when
// the template declares it and the request leaves it out — the JSON argument and the MCP tool as
// the shell does — with the session it signs as, or the key name it acts as; a value given is
// kept (SPEC §8.7).
func TestSay_FillsBySessionFromTheIdentityItSignsAs(t *testing.T) {
	r := testrepo.New(t)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	r.SetKeyring(t, ring)
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.WriteConfig(t, raw)
	r.Use(t)
	subject := publishOne(t, r).FotonID

	for _, tc := range []struct{ as, given, want string }{
		{"", "", testrepo.SessionID},
		{"lane-a", "", "lane-a"},
		{"", "someone-else", "someone-else"},
	} {
		fields := map[string]string{"step": "analysis"}
		if tc.given != "" {
			fields["by-session"] = tc.given
		}
		res, out, err := Say(context.Background(), nil, SayRequest{Subject: subject, Template: "working-on", As: tc.as, Fields: fields})
		if err != nil || res.IsError {
			t.Fatalf("say as=%q: %v %s", tc.as, err, errText(res))
		}
		if _, set := fields["by-session"]; set != (tc.given != "") {
			t.Errorf("say as=%q changed the caller's fields: %+v", tc.as, fields)
		}
		_, ans, err := Ask(context.Background(), nil, AskRequest{Query: "about", Ref: subject})
		if err != nil {
			t.Fatal(err)
		}
		var got any
		for _, c := range ans.Claims {
			if c.ID == out.ClaimID {
				got = c.Object.(map[string]any)["by-session"]
			}
		}
		if got != tc.want {
			t.Errorf("say as=%q given=%q: by-session %v, want %q", tc.as, tc.given, got, tc.want)
		}
	}
}
