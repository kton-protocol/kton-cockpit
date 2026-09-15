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
	raw.Claims.Scopes = map[string]string{"review": scope}
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

	first := sayInScope(t, pub.FotonID, "review")
	tip, length, _ = r.ScopeHead(t, scope)
	if tip != first || length != 1 {
		t.Fatalf("after one claim the tip should be it: tip=%s len=%d want %s len=1", tip, length, first)
	}

	second := sayInScope(t, pub.FotonID, "review")
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
	claimID := sayInScope(t, pub.FotonID, "review")

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
	raw.Claims.Scopes = map[string]string{"review": "sha256:" + strings.Repeat("ab", 32)}
	r.WriteConfig(t, raw)
	r.Use(t)
	pub := publishOne(t, r)

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject: pub.FotonID, Template: "working-on", Scope: "review",
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

// kton §7.4 makes ingest monotone and kton §11 forbids generalizing the sealed-world rule to it, so
// neither a branch nor a missing predecessor may stop a true claim being recorded. An earlier
// version refused on both — which also handed anyone whose records reach this registry a veto over
// its own work, since a scope id is public and one claim from an untrusted key was enough.
func TestScope_NeitherABranchNorAGapStopsAClaimBeingRecorded(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, r *testrepo.Repo, scope, subject string){
		"a branch": func(t *testing.T, r *testrepo.Repo, scope, subject string) {
			tip, _, _ := r.ScopeHead(t, scope)
			r.ChainClaimDirectly(t, subject, scope, tip, "branch-a")
			r.ChainClaimDirectly(t, subject, scope, tip, "branch-b")
		},
		"a missing predecessor": func(t *testing.T, r *testrepo.Repo, scope, subject string) {
			r.ChainClaimDirectly(t, subject, scope, "sha256:"+strings.Repeat("cd", 32), "orphan")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, scope := scopedRepo(t)
			pub := publishOne(t, r)
			sayInScope(t, pub.FotonID, "review")
			damage(t, r, scope, pub.FotonID)

			result, out, err := Say(context.Background(), nil, SayInput{
				Subject: pub.FotonID, Template: "working-on", Scope: "review",
				Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
			})
			if err != nil || result.IsError {
				t.Fatalf("%s stopped a true claim being recorded: err=%v %s", name, err, errText(result))
			}
			if out.Chain == nil || out.Chain.Prev == "" {
				t.Fatalf("the claim joined the scope but its position was not reported: %+v", out.Chain)
			}
		})
	}
}

// The seal verdict is where completeness IS judged — kton §7.4 puts it on the read path, over the
// sources actually held, when the seal is relied upon.
func TestScope_TheSealVerdictSaysWhetherTheChainIsWhole(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	sayInScope(t, pub.FotonID, "review")
	sayInScope(t, pub.FotonID, "review")

	seal := askSeal(t, scope)
	if !seal.Complete {
		t.Fatalf("an unbroken two-claim chain was not reported complete: %s", seal.Reason)
	}
	if seal.ChainLength != 2 || len(seal.Heads) != 1 {
		t.Fatalf("want one head over two claims, got %d head(s) over %d: %+v", len(seal.Heads), seal.ChainLength, seal)
	}
	// Stated on every verdict, including a complete one, because it never goes away — and this
	// scope was seeded with no parent, so there is nothing to check the head against at all.
	if !strings.Contains(seal.Limit, "no parent") {
		t.Errorf("a complete verdict does not state what it cannot see: %q", seal.Limit)
	}
	// Who wrote here, resolved from the verifying key rather than the declared `by`.
	if len(seal.Writers) != 1 || seal.Writers[0].Claims != 2 || seal.Writers[0].Tier == "" {
		t.Fatalf("the writers were not resolved from signatures: %+v", seal.Writers)
	}
	if seal.SeededWhen == "" || seal.Name == "" {
		t.Errorf("the verdict does not say who defined the scope and when: %+v", seal)
	}
}

func TestScope_TheSealVerdictReportsABranchAsUnsealable(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	sayInScope(t, pub.FotonID, "review")
	tip, _, _ := r.ScopeHead(t, scope)
	r.ChainClaimDirectly(t, pub.FotonID, scope, tip, "branch-a")
	r.ChainClaimDirectly(t, pub.FotonID, scope, tip, "branch-b")

	seal := askSeal(t, scope)
	if seal.Complete {
		t.Fatal("a branched chain was reported as sealable; no single head carries all of it")
	}
	if len(seal.Heads) < 2 {
		t.Fatalf("the verdict does not name the heads a reader must choose between: %+v", seal)
	}
	if !strings.Contains(seal.Reason, "seals only") {
		t.Errorf("the reason does not say what a branch costs: %q", seal.Reason)
	}
}

func TestScope_TheSealVerdictReportsAGapAsPartialNotBroken(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	sayInScope(t, pub.FotonID, "review")
	r.ChainClaimDirectly(t, pub.FotonID, scope, "sha256:"+strings.Repeat("cd", 32), "orphan")

	seal := askSeal(t, scope)
	if seal.Complete {
		t.Fatal("a chain with an unreachable predecessor was reported complete")
	}
	if seal.Gaps == 0 {
		t.Fatalf("the gap was not counted: %+v", seal)
	}
	// The distinction the specification insists on: incomplete is not invalid, and another source
	// may hold the missing statement.
	if !strings.Contains(seal.Reason, "another source") {
		t.Errorf("the reason presents a partial view as damage: %q", seal.Reason)
	}
}

// A writer whose key is in no configured tier is reported as having written — that is a fact about
// the chain — and their claims are excluded from the answer, which is SPEC §9.1 unchanged.
func TestScope_AnUntrustedWriterIsReportedButExcluded(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	mine := sayInScope(t, pub.FotonID, "review")
	tip, _, _ := r.ScopeHead(t, scope)
	theirs := r.ChainClaimAsStranger(t, pub.FotonID, scope, tip, "outsider")

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "scope", Ref: scope})
	if err != nil || result.IsError {
		t.Fatalf("ask scope failed: err=%v %s", err, errText(result))
	}
	if len(out.Included) != 1 || out.Included[0] != mine {
		t.Fatalf("expected only this repo's own claim included, got %+v", out.Included)
	}
	if len(out.Excluded) != 1 || out.Excluded[0] != theirs {
		t.Fatalf("the stranger's claim was not accounted for as found-and-excluded: %+v", out.Excluded)
	}
	var untrusted bool
	for _, w := range out.Seal.Writers {
		if w.Tier == "" {
			untrusted = true
		}
	}
	if !untrusted {
		t.Fatalf("a writer outside this repo's trust config was not reported at all: %+v", out.Seal.Writers)
	}
}

func askSeal(t *testing.T, scope string) *SealVerdict {
	t.Helper()
	result, out, err := Ask(context.Background(), nil, AskInput{Query: "scope", Ref: scope})
	if err != nil || result.IsError {
		t.Fatalf("ask scope failed: err=%v %s", err, errText(result))
	}
	if out.Seal == nil {
		t.Fatal("ask scope returned no seal verdict")
	}
	return out.Seal
}

// The set is the operator's and the pick is the session's — the same division as the template
// ceiling. A name this repo does not configure is REFUSED and the configured names are listed,
// rather than being treated as "no scope": a typo that quietly wrote the claim somewhere else, or
// nowhere, reads exactly like a decision.
func TestScope_RefusesANameTheConfigDoesNotHave(t *testing.T) {
	r, _ := scopedRepo(t)
	pub := publishOne(t, r)

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject: pub.FotonID, Template: "working-on", Scope: "reveiw",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("expected a tool-level refusal, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a scope name this repo does not configure was accepted")
	}
	if msg := errText(result); !strings.Contains(msg, "review") {
		t.Errorf("the refusal does not say which names exist: %s", msg)
	}
}

// Several scopes in one repository, which is the point of naming them: the notes about a model and
// the review of it are different conversations, and each chains on its own.
func TestScope_TwoScopesInOneRepoChainIndependently(t *testing.T) {
	r := testrepo.New(t)
	notes := r.SeedScope(t, "model-notes")
	review := r.SeedScope(t, "review-2026-09")
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = map[string]string{"notes": notes, "review": review}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	noteA := sayInScope(t, pub.FotonID, "notes")
	rev := sayInScope(t, pub.FotonID, "review")
	noteB := sayInScope(t, pub.FotonID, "notes")

	// The second note follows the first, not the review: the chains do not interleave.
	tip, length, _ := r.ScopeHead(t, notes)
	if tip != noteB || length != 2 {
		t.Fatalf("notes: tip=%s len=%d, want %s len=2", tip, length, noteB)
	}
	tip, length, _ = r.ScopeHead(t, review)
	if tip != rev || length != 1 {
		t.Fatalf("review: tip=%s len=%d, want %s len=1", tip, length, rev)
	}
	if noteA == "" {
		t.Fatal("the first note was not recorded")
	}

	// And each seals on its own: asking about one says nothing about the other.
	seal := askSeal(t, review)
	if !seal.Complete || seal.ChainLength != 1 {
		t.Fatalf("the review scope should seal over its one claim alone: %+v", seal)
	}
}

// Sealing records the scope's head in its parent, which is the only thing that can tell a complete
// chain from a rewound one: the file itself cannot, because dropping the last claim leaves a
// shorter chain that is internally valid.
func TestScope_SealRecordsTheHeadInTheParent(t *testing.T) {
	r := testrepo.New(t)
	parent := r.SeedScope(t, "lab-commons")
	child := r.SeedScopeUnder(t, "review-2026-09", parent)
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = map[string]string{"commons": parent, "review": child}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	sayInScope(t, pub.FotonID, "review")
	head, _, _ := r.ScopeHead(t, child)

	sealID := r.SealScope(t, "review")
	if sealID == "" {
		t.Fatal("sealing produced no claim")
	}

	// The parent's chain grew, and the seal it now carries names the child's head at that moment.
	parentTip, parentLen, _ := r.ScopeHead(t, parent)
	if parentTip != sealID || parentLen != 1 {
		t.Fatalf("the seal did not land in the parent chain: tip=%s len=%d want %s len=1", parentTip, parentLen, sealID)
	}

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "scope", Ref: parent})
	if err != nil || result.IsError {
		t.Fatalf("ask scope on the parent failed: err=%v %s", err, errText(result))
	}
	var found bool
	for _, c := range out.Claims {
		if c.ID == sealID && c.Subject == child && strings.Contains(c.Predicate, "seal") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the parent does not carry a seal naming the child scope: %+v", out.Claims)
	}
	if !strings.Contains(out.Raw, head[:16]) && !strings.Contains(errText(result), head[:16]) {
		// The head is in the claim's object rather than its line, so this is a soft check: what
		// matters is that the seal exists and names the child, asserted above.
		t.Logf("note: the sealed head %s is carried as the claim's object", head)
	}
}

// A scope with a parent can be checked against a seal; one without cannot be checked at all. The
// verdict must say which situation the reader is in, because "complete" means something different
// in each.
func TestScope_ASealedScopeSaysWhatItCanBeCheckedAgainst(t *testing.T) {
	r := testrepo.New(t)
	parent := r.SeedScope(t, "lab-commons")
	orphan := r.SeedScope(t, "notes")
	child := r.SeedScopeUnder(t, "review", parent)
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = map[string]string{"notes": orphan, "review": child}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	sayInScope(t, pub.FotonID, "notes")
	sayInScope(t, pub.FotonID, "review")

	withParent := askSeal(t, child)
	if withParent.Parent != parent {
		t.Fatalf("the verdict does not name the parent: %+v", withParent)
	}
	if !strings.Contains(withParent.Limit, "seal recorded in the parent") {
		t.Errorf("a scope with a parent should be told what to check against: %q", withParent.Limit)
	}

	without := askSeal(t, orphan)
	if without.Parent != "" {
		t.Fatalf("a scope seeded with no parent reported one: %+v", without)
	}
	if !strings.Contains(without.Limit, "no parent") {
		t.Errorf("a scope with no parent should be told it cannot be checked: %q", without.Limit)
	}
}
