package scope

// Package scope answers the question a scope exists for: does this chain reach its seed without a
// gap, over the sources actually held, and who wrote into it?
//
// kton §7.4 puts the judgment exactly here. Ingest is monotone and accepts a scoped statement whose
// prev is not yet resolvable, because the missing one may live in another source; the closed-world
// guarantee is "a seal-verification judgment, evaluated over the resolved union of sources WHEN THE
// SEAL IS RELIED UPON". Relying on it is a read, so this is a read.
//
// And kton §7.4 leaves it to a consumer in as many words — "sealing rules are checked by
// consumers/aggregators, not the kernel". That makes this the cockpit's work rather than something
// to request upstream, which is the opposite of the usual answer in this repository.
//
// It lives in its own package because two callers need the same answer and must not drift: the
// `scope` query a session asks, and `cockpit scope read`, where an operator judges a file somebody
// sent. The second is the reason the verdict takes a Runner rather than reading a fixed registry —
// a received scope is read where it lies, against THIS repository's trust configuration.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/verify"
)

// SealVerdict is what a reader needs before relying on a scope: whether the chain is whole, who
// defined it, and what the check cannot see.
type Verdict struct {
	Scope string `json:"scope"`
	// Name is the human label the seed carries; the scope's identity is the hash, never this.
	Name string `json:"name,omitempty"`
	// SeededBy and SeededWhen come from the genesis statement. Membership is fixed by the seed.
	SeededBy   string `json:"seededBy,omitempty"`
	SeededWhen string `json:"seededWhen,omitempty"`
	// Parent is the scope this one was seeded under, if any. Only a scope with a parent can be
	// sealed: a seal records a head THERE, and the seed covers its parent, so it cannot be added
	// after the fact.
	Parent string `json:"parent,omitempty"`
	// Responsible is the identities the seed names as belonging here (kton §7.4). Its MEANING is
	// convention, not kernel — reported as written, never enforced. Empty is the common case: the
	// reference implementation emits none.
	Responsible []string `json:"responsible,omitempty"`

	// Complete is the seal verdict: every claim naming this scope resolves back to the seed, and
	// exactly one head carries all of them.
	Complete bool `json:"complete"`
	// Heads is every tip the chain has. More than one means no single head seals the whole: each
	// commits only to the claims on its own branch, and there is no operation that rejoins them.
	Heads       []string `json:"heads"`
	ChainLength int      `json:"chainLength"`
	// Gaps counts claims whose prev this registry does not hold. Adding a source can only resolve
	// more of them, so a gap says the view is partial — not that anything is invalid.
	Gaps int `json:"gaps,omitempty"`
	// Reason says, in one sentence, what makes the verdict what it is.
	Reason string `json:"reason"`
	// Limit is stated on every verdict, including a complete one, because it never goes away: a
	// withheld LAST claim leaves a shorter chain that is internally valid and points at nothing
	// missing. No in-band check finds it.
	Limit string `json:"limit"`
	// Writers is every key that actually signed into this scope, with the trust tier this repo
	// resolved for it — the "who wrote here" half, answered from signatures rather than from the
	// `by` each statement declares about itself.
	Writers []Writer `json:"writers,omitempty"`
}

// ScopeWriter is one signing identity that has written into a scope, and what this repo makes of it.
type Writer struct {
	KeyID  string `json:"keyid"`
	Tier   string `json:"tier,omitempty"`
	Claims int    `json:"claims"`
}

// tailLimit states what no in-band check can see. It comes in two versions because a scope with a
// parent has somewhere to be checked AGAINST, and one without does not.
//
// Dropping the last claim leaves a shorter chain that is internally valid and points at nothing
// missing, so the file alone can never reveal it. A seal fixes that: the parent carries the head as
// of the moment of sealing, so a chain rewound behind it no longer matches. Sealing is repeatable
// and meant to be — each one moves the point beyond which a rewind is detectable.
const tailLimit = "a withheld LAST claim cannot be detected from this file: dropping it leaves a " +
	"shorter chain that is internally valid and points at nothing missing. This scope names no " +
	"parent, so there is no recorded head to compare against — nothing here can tell a complete " +
	"chain from a rewound one."

const tailLimitSealed = "a withheld LAST claim cannot be detected from this file alone. Compare the " +
	"head above against the seal recorded in the parent: everything up to the last seal is fixed, " +
	"because a rewind behind it no longer matches what the parent carries. Anything added SINCE " +
	"that seal is not yet covered."

// ClaimVerification is one claim in the scope and what this repository makes of its signature.
type ClaimVerification struct {
	Claim binaries.ClaimAxis
	Tier  string
}

// Describe verifies a scope's seal and reports who wrote into it.
func Describe(ctx context.Context, cfg *config.Config, r *binaries.Runner, scopeID string) (*Verdict, []ClaimVerification, error) {
	seed, err := r.Seed(ctx, scopeID)
	if err != nil {
		return nil, nil, fmt.Errorf("no readable scope seed at %s: %w", scopeID, err)
	}
	head, err := r.Head(ctx, scopeID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the head of scope %s failed: %w", scopeID, err)
	}
	claims, err := r.ScopeChain(ctx, scopeID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the claims of scope %s failed: %w", scopeID, err)
	}

	v := &Verdict{
		Scope: scopeID, Name: seed.Name, SeededBy: seed.By, SeededWhen: seed.When,
		Parent: seed.Parent, Responsible: seed.Responsible,
		Heads: head.Heads, ChainLength: head.ChainLength,
		Gaps: head.Unresolved, Limit: tailLimit,
	}
	if seed.Parent != "" {
		v.Limit = tailLimitSealed
	}

	// Derived from the substrate's own resolution rather than by walking prev here. `unresolved`
	// already counts every claim whose predecessor this registry cannot resolve, and a single head
	// means one tip commits to all of them — so no claim can hang off the seed's line unreached.
	// Re-deriving that by hand would be a second implementation of the kernel's chain resolution,
	// disagreeing with it in exactly the cases that matter.
	switch {
	case head.Unresolved > 0 && len(head.Heads) > 1:
		v.Reason = fmt.Sprintf("%d claim(s) have a predecessor this registry does not hold, and the chain "+
			"has %d heads — neither the order nor its completeness can be relied on here", head.Unresolved, len(head.Heads))
	case head.Unresolved > 0:
		v.Reason = fmt.Sprintf("%d claim(s) name this scope with a predecessor this registry does not hold; "+
			"the missing statement may live in another source, so this view is partial rather than broken", head.Unresolved)
	case len(head.Heads) > 1:
		v.Reason = fmt.Sprintf("the chain has %d heads: claims share a predecessor, so each head seals only "+
			"its own branch and none seals the whole. A claim carries one prev, so nothing rejoins them", len(head.Heads))
	default:
		v.Complete = true
		v.Reason = fmt.Sprintf("every claim resolves back to the seed and one head (%s) carries all %d of them",
			head.Heads[0], head.ChainLength)
	}

	dir, derr := os.MkdirTemp("", "cockpit-scope-")
	if derr != nil {
		return nil, nil, derr
	}
	defer os.RemoveAll(dir)

	byKey := map[string]*Writer{}
	verified := make([]ClaimVerification, 0, len(claims))
	for i, c := range claims {
		// Verified through the envelope rather than the id: a claim whose predecessor is not held
		// here is stored but not retrievable by id, and it is exactly the claim a gap is about.
		envPath := filepath.Join(dir, fmt.Sprintf("%d.dsse.json", i))
		if werr := os.WriteFile(envPath, c.Envelope, 0o600); werr != nil {
			return nil, nil, werr
		}
		tier, verifyingKey, verr := verify.ResolveTier(ctx, r, cfg, envPath, verify.Claim)
		if verr != nil {
			return nil, nil, fmt.Errorf("verifying %s failed: %w", c.ID, verr)
		}
		keyid := "(unverified)"
		if tier != verify.Untrusted {
			if id, kerr := r.KeyID(ctx, verifyingKey); kerr == nil {
				keyid = id
			}
		}
		w, ok := byKey[keyid]
		if !ok {
			w = &Writer{KeyID: keyid, Tier: tier}
			byKey[keyid] = w
		}
		w.Claims++
		verified = append(verified, ClaimVerification{Claim: c.ClaimAxis, Tier: tier})
	}
	for _, w := range byKey {
		v.Writers = append(v.Writers, *w)
	}
	sort.Slice(v.Writers, func(i, j int) bool { return v.Writers[i].KeyID < v.Writers[j].KeyID })
	return v, verified, nil
}

// Line renders the verdict for a human.
func (v *Verdict) Line() string {
	var b strings.Builder
	state := "INCOMPLETE"
	if v.Complete {
		state = "complete"
	}
	fmt.Fprintf(&b, "scope %s", v.Scope)
	if v.Name != "" {
		fmt.Fprintf(&b, " (%q)", v.Name)
	}
	fmt.Fprintf(&b, "\nseal:    %s — %s\n", state, v.Reason)
	if v.Parent != "" {
		fmt.Fprintf(&b, "parent:  %s\n", v.Parent)
	}
	for _, w := range v.Writers {
		tier := w.Tier
		if tier == "" {
			tier = "not trusted here"
		}
		fmt.Fprintf(&b, "writer:  %s  %d claim(s)  [%s]\n", w.KeyID, w.Claims, tier)
	}
	fmt.Fprintf(&b, "limit:   %s", v.Limit)
	return b.String()
}
