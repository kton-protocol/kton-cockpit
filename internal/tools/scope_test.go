package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/testrepo"
)

// A scope is what turns a session's claims from a pile into one object. These cover the whole of
// that: claims chain in order under a configured scope, the tip advances, and the two conditions
// that make a chain unextendable are refusals rather than silent choices.

func scopedRepo(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	r := testrepo.New(t)
	// Seeded before any config points at it, because that is the real order: `nekton seed` is an
	// operator action and not one of the three verbs, so the scope exists before the session does.
	scope := r.SeedScope(t, "review-2026-09")
	raw := testrepo.DefaultConfig()
	raw.Claims.Scope = scope
	r.WriteConfig(t, raw)
	r.Use(t)
	return r, scope
}

func TestScope_ClaimsChainInOrderAndAdvanceTheTip(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)

	// An empty scope's tip is the seed itself, so the first claim chains onto the seed.
	tip, length, _ := r.ScopeHead(t, scope)
	if tip != scope || length != 0 {
		t.Fatalf("a freshly seeded scope should be its own tip with nothing chained, got tip=%s len=%d", tip, length)
	}

	first := sayWorkingOn(t, pub.FotonID)
	tip, length, _ = r.ScopeHead(t, scope)
	if tip != first || length != 1 {
		t.Fatalf("after one claim the tip should be it: tip=%s len=%d want %s len=1", tip, length, first)
	}

	second := sayWorkingOn(t, pub.FotonID)
	tip, length, _ = r.ScopeHead(t, scope)
	if tip != second || length != 2 {
		t.Fatalf("after two claims the tip should be the second: tip=%s len=%d want %s len=2", tip, length, second)
	}
	// Linear, not branched: the second chained onto the first rather than onto the seed. A cockpit
	// that read the tip once and reused it would produce two claims sharing a prev, and the chain
	// would fork without anything failing.
	if _, _, branched := r.ScopeHead(t, scope); branched {
		t.Fatal("two claims from the same session branched the scope; each must chain onto the previous tip")
	}
}

// The default is unchanged: with no scope configured, claims stand on their own and nothing is
// chained. The feature must not quietly start structuring a repo that did not ask for it.
func TestScope_UnconfiguredLeavesClaimsUnchained(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil || result.IsError {
		t.Fatalf("ask failed: err=%v %s", err, errText(result))
	}
	for _, c := range out.Claims {
		if c.ID == claimID && c.Scope != "" {
			t.Fatalf("an unconfigured repo chained its claim under scope %q", c.Scope)
		}
	}
}

// The scope filter in `ask` was, until claims could be chained, a dimension with no producer: this
// cockpit could narrow by something it was unable to write. Both halves now meet.
func TestScope_TheAskFilterFindsWhatSayChained(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	result, out, err := Ask(context.Background(), nil, AskInput{
		Query: "about", Ref: pub.FotonID,
		Filter: &AskFilter{Scope: scope},
	})
	if err != nil || result.IsError {
		t.Fatalf("ask failed: err=%v %s", err, errText(result))
	}
	if len(out.Included) != 1 || out.Included[0] != claimID {
		t.Fatalf("the scope filter did not find the claim that was chained under it: %+v", out.Included)
	}

	// And the negative control: a different, valid-looking scope id must exclude it rather than
	// match everything.
	other := r.SeedScope(t, "some-other-review")
	result, out, err = Ask(context.Background(), nil, AskInput{
		Query: "about", Ref: pub.FotonID,
		Filter: &AskFilter{Scope: other},
	})
	if err != nil || result.IsError {
		t.Fatalf("ask failed: err=%v %s", err, errText(result))
	}
	if len(out.Included) != 0 {
		t.Fatalf("filtering by a scope the claim is not in returned it anyway: %+v", out.Included)
	}
}

// A configured scope that does not exist in this registry is refused with the command that creates
// one, rather than producing a claim the store then declines.
func TestScope_RefusesAScopeThisRegistryDoesNotHold(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.Scope = "sha256:" + strings.Repeat("ab", 32)
	r.WriteConfig(t, raw)
	r.Use(t)
	pub := publishOne(t, r)

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject: pub.FotonID, Template: "working-on",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("expected a tool-level refusal, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("say accepted a scope this registry does not hold")
	}
	if msg := errText(result); !strings.Contains(msg, "nekton seed") {
		t.Errorf("the refusal does not say how to create the scope: %s", msg)
	}
}

// A branched scope is the case the substrate deliberately leaves open: it reports the structure and
// prescribes no remedy, so the choice lands here. Picking a head silently would make which branch a
// claim belongs to depend on which session ran first.
func TestScope_RefusesABranchedScope(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	sayWorkingOn(t, pub.FotonID)

	// Fork it behind the cockpit's back, the way a mirror bringing in a peer's branch would: two
	// claims chained onto the same prev.
	tip, _, _ := r.ScopeHead(t, scope)
	r.ChainClaimDirectly(t, pub.FotonID, scope, tip, "first-branch")
	r.ChainClaimDirectly(t, pub.FotonID, scope, tip, "second-branch")
	if _, _, branched := r.ScopeHead(t, scope); !branched {
		t.Fatal("the fixture did not manage to branch the scope; the rest of this test would assert nothing")
	}

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject: pub.FotonID, Template: "working-on",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("expected a tool-level refusal, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("say extended a branched scope, choosing a branch by running order")
	}
	msg := errText(result)
	if !strings.Contains(msg, "BRANCHED") {
		t.Errorf("the refusal does not name the condition: %s", msg)
	}
	// Both heads named, because the person resolving this needs to know what they are choosing
	// between, and the tool is not going to choose for them.
	if strings.Count(msg, "sha256:") < 3 {
		t.Errorf("the refusal does not name the scope and both heads: %s", msg)
	}
}

// The other refusal, and the worse of the two. A claim naming this scope whose prev is not held
// here leaves the reported tip PROVISIONAL: the real head may sit behind a claim this store has
// never seen. Chaining onto a provisional tip does not inherit a fork — it creates one the moment
// the missing claims arrive.
func TestScope_RefusesAScopeWithUnresolvedClaims(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)

	// A claim chained onto a prev nothing holds. The substrate PERSISTS it — unresolved is
	// incomplete, not invalid — which is exactly why the condition has to be checked rather than
	// assumed away by a successful write.
	r.ChainClaimDirectly(t, pub.FotonID, scope, "sha256:"+strings.Repeat("cd", 32), "orphan")

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject: pub.FotonID, Template: "working-on",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("expected a tool-level refusal, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("say chained onto a provisional tip; the chain would fork when the missing claim arrives")
	}
	if msg := errText(result); !strings.Contains(msg, "PROVISIONAL") {
		t.Errorf("the refusal does not say why the tip cannot be trusted: %s", msg)
	}
}
