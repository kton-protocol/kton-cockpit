package cockpit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

func working(scope string, i int) SayRequest {
	return SayRequest{Subject: "urn:example:item", Template: "working-on", Scope: scope,
		Fields: map[string]string{"step": fmt.Sprintf("analysis-%d", i), "by-session": testrepo.SessionID}}
}

// A batch says many claims in one write: the ids come back in order, claims into a scope chain one
// after another in that order, and the scope keeps one head; the claims can be asked for after
// (SPEC §8.9).
func TestSayBatch_ManyClaimsInOneWriteChainInOrder(t *testing.T) {
	r, scope := scopedRepo(t)
	ctx := context.Background()
	c := New(Start{})
	first := sayInScope(t, publishOne(t, r).FotonID, "review")

	const n = 25
	var in SayBatchRequest
	for i := 0; i < n; i++ {
		in.Claims = append(in.Claims, working("review", i))
	}
	in.Claims = append(in.Claims, SayRequest{Subject: "urn:example:other", Template: "working-on",
		Fields: map[string]string{"step": "unscoped", "by-session": testrepo.SessionID}})
	out, err := c.SayBatch(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Claims) != n+1 {
		t.Fatalf("want %d answers, got %d", n+1, len(out.Claims))
	}
	prev := first
	for i := 0; i < n; i++ {
		got := out.Claims[i]
		if !strings.HasPrefix(got.ClaimID, "sha256:") || got.Chain == nil || got.Chain.Prev != prev {
			t.Fatalf("claim %d: want chained onto %s, got %+v", i+1, prev, got)
		}
		prev = got.ClaimID
	}
	if out.Claims[n].Chain != nil {
		t.Errorf("an unscoped claim of the batch got a chain: %+v", out.Claims[n].Chain)
	}
	if tip, length, branched := r.ScopeHead(t, scope); tip != prev || length != n+1 || branched {
		t.Errorf("the scope after the batch: tip %s length %d branched %v, want %s, %d, false", tip, length, branched, prev, n+1)
	}
	about, err := c.Ask(ctx, AskRequest{Query: "by", Axis: "predicate", Ref: "https://kton.dev/v/working-on"})
	if err != nil {
		t.Fatal(err)
	}
	for _, cl := range out.Claims {
		if !strings.Contains(fmt.Sprint(about), cl.ClaimID) {
			t.Errorf("ask does not find %s", cl.ClaimID)
		}
	}
}

// Every claim is checked before any is signed: one that would be refused refuses the whole batch,
// names its position, and nothing is written (SPEC §8.9).
func TestSayBatch_OneBadClaimRefusesTheBatchAndWritesNothing(t *testing.T) {
	r, scope := scopedRepo(t)
	ctx := context.Background()
	c := New(Start{})
	for _, bad := range []SayRequest{
		{Subject: "urn:example:item", Template: "not-allowed"},
		{Template: "working-on", Fields: map[string]string{"step": "x"}},
		{Subject: "urn:example:item", Template: "working-on", Scope: "nowhere", Fields: map[string]string{"step": "x", "by-session": "s"}},
		{Subject: "urn:example:item", Template: "reproduces"},
	} {
		in := SayBatchRequest{Claims: []SayRequest{working("review", 1), working("review", 2), bad, working("review", 4)}}
		_, err := c.SayBatch(ctx, in)
		var ref *Refusal
		if !errors.As(err, &ref) || !strings.HasPrefix(ref.Reason, "claim 3: ") {
			t.Errorf("%+v: want a refusal naming claim 3, got %v", bad, err)
		}
		if tip, length, _ := r.ScopeHead(t, scope); tip != scope || length != 0 {
			t.Fatalf("%+v: a refused batch wrote: tip %s length %d", bad, tip, length)
		}
	}
	if _, err := c.SayBatch(ctx, SayBatchRequest{}); err == nil {
		t.Error("an empty batch: want refused")
	}
}

// replace: previous sees the earlier claims of the same batch; expectHead holds within a batch: the
// first claim of a scope names a head the scope has, each later one the claim before it.
func TestSayBatch_ReplacePreviousAndExpectHeadWithinTheBatch(t *testing.T) {
	r, scope := scopedRepo(t)
	r.Write(t, ".kton/templates/status.json", `{"name": "status", "target": "either", "replace": "previous",
  "predicate": "https://example.org/status", "fields": {"state": {"type": "string", "required": true}}}`)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "status")
	r.WriteConfig(t, raw)
	ctx := context.Background()
	c := New(Start{})
	status := func(state string) SayRequest {
		return SayRequest{Subject: "urn:example:item", Template: "status", Fields: map[string]string{"state": state}}
	}
	before, err := c.Say(ctx, status("open"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{status("doing"), status("done")}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Claims[0].Replaces != before.ClaimID || out.Claims[1].Replaces != out.Claims[0].ClaimID {
		t.Errorf("replace: previous in a batch: want %s then %s, got %q then %q",
			before.ClaimID, out.Claims[0].ClaimID, out.Claims[0].Replaces, out.Claims[1].Replaces)
	}

	a, b := working("review", 1), working("review", 2)
	a.ExpectHead = scope
	b.ExpectHead = scope // stale: the batch's first claim moved the head
	if _, err := c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{a, b}}); err == nil || !strings.Contains(err.Error(), "claim 2") {
		t.Errorf("a later claim naming the head the batch already moved: want refused at claim 2, got %v", err)
	}
	if _, length, _ := r.ScopeHead(t, scope); length != 0 {
		t.Fatalf("the refused batch wrote %d claims", length)
	}
	out, err = c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{a}})
	if err != nil {
		t.Fatal(err)
	}
	b.ExpectHead = out.Claims[0].ClaimID
	c2 := working("review", 3)
	c2.ExpectHead = ""
	if _, err := c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{b, c2}}); err != nil {
		t.Errorf("a batch whose first claim names the current head: %v", err)
	}
	if _, length, branched := r.ScopeHead(t, scope); length != 3 || branched {
		t.Errorf("after the batches: length %d branched %v, want 3, false", length, branched)
	}
}
