package cockpit

import (
	"context"
	"kton.dev/plankton/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A template with "replace": "previous" has say fill replaces with this signer's own newest claim of
// that template about the subject, and the answer names it (ADR-013 A1/A5, M2); another key's claim
// is never named; a replaces the request gives is kept.
func TestSay_ReplacePreviousNamesTheSignersOwnLastClaim(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, ".kton/templates/status.json", `{"name": "status", "target": "either", "replace": "previous",
  "predicate": "https://example.org/status", "fields": {"state": {"type": "string", "required": true}}}`)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "status")
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.SetKeyring(t, ring)
	r.WriteConfig(t, raw)
	r.Use(t)
	c := New(Start{})
	ctx := context.Background()
	const subject = "urn:example:item"
	say := func(as, state, replaces string) *SayResult {
		t.Helper()
		f := map[string]string{"state": state}
		if replaces != "" {
			f["replaces"] = replaces
		}
		out, err := c.Say(ctx, SayRequest{Subject: subject, Template: "status", As: as, Fields: f})
		if err != nil {
			t.Fatalf("say %s as %q: %v", state, as, err)
		}
		return out
	}

	first := say("", "draft", "")
	if first.Replaces != "" {
		t.Fatalf("the first claim replaces nothing, got %s", first.Replaces)
	}
	second := say("", "review", "")
	if second.Replaces != first.ClaimID {
		t.Fatalf("the second claim should replace the first (%s), got %q", first.ClaimID, second.Replaces)
	}
	// Another key's claim is not this signer's to retire.
	other := say("lane-a", "approved", "")
	if other.Replaces != "" {
		t.Fatalf("lane-a's first claim named another key's claim: %s", other.Replaces)
	}
	third := say("", "final", "")
	if third.Replaces != second.ClaimID {
		t.Fatalf("the third should replace the second (%s), not the first or lane-a's, got %q", second.ClaimID, third.Replaces)
	}
	// A replaces the request gives is kept, and not reported as filled.
	given := say("", "redo", first.ClaimID)
	if given.Replaces != "" {
		t.Fatalf("a given replaces is the request's, not filled: %q", given.Replaces)
	}
}

// A claim that only declares the signer's keyid, signed by another key, is not the signer's: say
// verifies the signature before naming a claim in replaces (kton-review P1 on #166).
func TestSay_ReplacePreviousNeverNamesAClaimThatOnlyDeclaresTheKeyid(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, ".kton/templates/status.json", `{"name": "status", "target": "either", "replace": "previous",
  "predicate": "https://example.org/status", "fields": {"state": {"type": "string", "required": true}}}`)
	ring := t.TempDir()
	lane := keyringKey(t, ring, "lane-a")
	r.Write(t, ".kton/keys/lanes/lane-a-claims.pub", lane+"\n")
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "status")
	raw.Trust.Tiers = append(raw.Trust.Tiers, "lanes")
	r.SetKeyring(t, ring)
	r.WriteConfig(t, raw)
	r.Use(t)
	c := New(Start{})
	ctx := context.Background()
	say := func(as, state string) *SayResult {
		t.Helper()
		out, err := c.Say(ctx, SayRequest{Subject: "urn:example:item", Template: "status", As: as, Fields: map[string]string{"state": state}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	approved := say("", "approved")
	time.Sleep(1100 * time.Millisecond) // lane-a's claim is the newer one, as an attacker's would be
	say("lane-a", "noted")

	// lane-a's stored claim now declares my keyid; its signature is still lane-a's.
	me, _, err := keyOf(r.Config(t).NektonKey)
	if err != nil {
		t.Fatal(err)
	}
	laneID := keyIDHexOf(t, lane)
	files, _ := filepath.Glob(filepath.Join(r.Root, ".kton", "nekton", "objects", "*", "*"))
	more, _ := filepath.Glob(filepath.Join(r.Root, ".kton", "nekton", "objects", "*"))
	rewrote := false
	for _, f := range append(files, more...) {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), laneID) {
			continue
		}
		os.WriteFile(f, []byte(strings.ReplaceAll(string(b), `"keyid":"`+laneID+`"`, `"keyid":"`+me+`"`)), 0o644)
		rewrote = true
	}
	if !rewrote {
		t.Fatal("found no stored claim declaring lane-a's keyid to rewrite")
	}
	if got := say("", "rejected").Replaces; got != approved.ClaimID {
		t.Fatalf("replaces names %s, want my own approved %s, not lane-a's relabelled claim", got, approved.ClaimID)
	}
}

// The newest claim that none of mine replaces is named, even when a newer one of mine is replaced
// (kton-review T3 on #166).
func TestNewestUnreplaced_SkipsAReplacedNewerClaim(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	mine := []ownClaim{{"sha256:a", t0}, {"sha256:b", t0.Add(time.Minute)}, {"sha256:c", t0.Add(2 * time.Minute)}}
	if got := newestUnreplaced(mine, map[string]bool{"sha256:c": true}); got != "sha256:b" {
		t.Fatalf("want sha256:b, got %s", got)
	}
	if got := newestUnreplaced(mine, nil); got != "sha256:c" {
		t.Fatalf("want sha256:c, got %s", got)
	}
}

func keyIDHexOf(t *testing.T, pubHex string) string {
	t.Helper()
	pub, err := core.ParsePublicKeyHex(pubHex)
	if err != nil {
		t.Fatal(err)
	}
	return core.KeyIDHex(pub)
}
