package cockpit

import (
	"context"
	"testing"

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
