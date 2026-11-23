package cockpit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A short id is refused loudly wherever say takes one, never signed and never dropped (userinterface,
// dry run 1 finding 6b3b60e3): as the subject, in a field of any type, in a batch, where nothing of the
// batch is written. The refusal names the one record or claim a unique prefix is the start of.
func TestSay_AShortIDIsRefusedNamingTheWholeOne(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	r.WriteConfig(t, raw)
	r.Use(t)
	ctx := context.Background()
	c := New(Start{})
	subj := "sha256:" + strings.Repeat("a", 64)
	first, err := c.Say(ctx, SayRequest{Subject: subj, Template: "describes", Fields: map[string]string{"value": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	short := first.ClaimID[:15] // sha256: and 8 hex digits, as WORKGROUP.md prints it
	count := func() int {
		t.Helper()
		a, err := c.Ask(ctx, AskRequest{Query: "about", Ref: subj})
		if err != nil {
			t.Fatal(err)
		}
		return len(a.Claims)
	}
	before := count()
	refused := func(what string, err error, wantHint bool) {
		t.Helper()
		var ref *Refusal
		if !errors.As(err, &ref) || ref.Code != "argument" {
			t.Fatalf("%s: want a refusal, got %v", what, err)
		}
		if wantHint && !strings.Contains(ref.Reason, first.ClaimID) {
			t.Errorf("%s: the refusal names the whole id %s: %s", what, first.ClaimID, ref.Reason)
		}
	}
	_, err = c.Say(ctx, SayRequest{Subject: short, Template: "describes", Fields: map[string]string{"value": "b"}})
	refused("a short subject", err, true)
	_, err = c.Say(ctx, SayRequest{Subject: subj, Template: "describes", Fields: map[string]string{"value": short}})
	refused("a short id in a string field", err, true)
	_, err = c.Say(ctx, SayRequest{Subject: subj, Template: "describes", Fields: map[string]string{"value": "sha256:ffffffff"}})
	refused("a short id nothing begins with", err, false)
	_, err = c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{
		{Subject: subj, Template: "describes", Fields: map[string]string{"value": "c"}},
		{Subject: subj, Template: "describes", Fields: map[string]string{"value": short}},
	}})
	refused("a batch with a short id", err, true)
	if err != nil && !strings.Contains(err.Error(), "claim 2") {
		t.Errorf("a batch's refusal names the line: %v", err)
	}
	if n := count(); n != before {
		t.Errorf("nothing refused is written, nor the rest of its batch: %d claims, %d before", n, before)
	}
	// A whole id in a field is taken as it is.
	if _, err := c.Say(ctx, SayRequest{Subject: subj, Template: "describes", Fields: map[string]string{"value": first.ClaimID}}); err != nil {
		t.Errorf("a whole id: %v", err)
	}
}
