package cockpit

import (
	"context"
	"strings"
	"testing"
)

// With expectHead a claim joins its scope only on the head the caller saw: chained onto exactly that
// head, refused once another claim moved it, and refused without a scope or with something that is
// not a claim id.
func TestScope_ExpectHeadAppendsOnlyToTheHeadTheCallerSaw(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	say := func(expect string) (*SayResult, string) {
		t.Helper()
		res, out, err := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: "working-on",
			Scope: "review", ExpectHead: expect, Fields: map[string]string{"step": "analysis", "by-session": "s"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			return nil, errText(res)
		}
		return &out, ""
	}

	first, msg := say(scope) // an empty scope's head is its seed
	if first == nil || first.Chain.Prev != scope {
		t.Fatalf("on the seed: want it chained onto the seed: %+v %s", first, msg)
	}
	second, msg := say(first.ClaimID)
	if second == nil || second.Chain.Prev != first.ClaimID {
		t.Fatalf("on the head it saw: want it chained there: %+v %s", second, msg)
	}
	if out, msg := say(first.ClaimID); out != nil || !strings.Contains(msg, "no longer") || !strings.Contains(msg, second.ClaimID) {
		t.Fatalf("on a head that moved: want a refusal naming the head now: %+v %s", out, msg)
	}
	if tip, length, _ := r.ScopeHead(t, scope); tip != second.ClaimID || length != 2 {
		t.Fatalf("a refused claim must not be written: tip=%s len=%d", tip, length)
	}

	res, _, _ := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: "working-on",
		ExpectHead: second.ClaimID, Fields: map[string]string{"step": "analysis", "by-session": "s"}})
	if res == nil || !res.IsError {
		t.Error("expectHead without a scope: want a refusal")
	}
	if out, msg := say("not-a-claim"); out != nil || !strings.Contains(msg, "claim id") {
		t.Errorf("expectHead that is no claim id: want a refusal, got %+v %s", out, msg)
	}
}
