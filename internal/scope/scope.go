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

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
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
	// SeedTier is the trust tier of the key that signed the seed, resolved here. A seed is read
	// over the federation sources too, so it is anybody's word until it verifies: one no tier holds
	// reports only its id, never its name, signer, parent or responsible.
	SeedTier string `json:"seedTier"`
	// Context is the scope's parents, nearest first (study → project → programme), each verified
	// the same way. It ends at a seed with no parent, one no source holds, one that does not
	// verify, or a seed already on the path.
	Context []Ancestor `json:"context,omitempty"`

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

// Ancestor is one scope above another in the parent chain.
type Ancestor struct {
	Scope      string `json:"scope"`
	Name       string `json:"name,omitempty"`
	SeededBy   string `json:"seededBy,omitempty"`
	SeededWhen string `json:"seededWhen,omitempty"`
	Tier       string `json:"tier"`
	// Missing is a parent no source holds a seed for: the chain goes on, but not here.
	Missing bool `json:"missing,omitempty"`
}

// maxContext bounds the parent walk; a real hierarchy is a handful deep.
const maxContext = 32

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
func Describe(ctx context.Context, cfg *config.Config, r index.Reader, scopeID string) (*Verdict, []ClaimVerification, error) {
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

	dir, derr := os.MkdirTemp("", "cockpit-scope-")
	if derr != nil {
		return nil, nil, derr
	}
	defer os.RemoveAll(dir)

	seedTier, err := seedTierOf(ctx, cfg, r, dir, seed)
	if err != nil {
		return nil, nil, err
	}
	v := &Verdict{
		Scope: scopeID, SeedTier: seedTier,
		Heads: head.Heads, ChainLength: head.ChainLength,
		Gaps: head.Unresolved, Limit: tailLimit,
	}
	if seedTier != verify.Untrusted {
		v.Name, v.SeededBy, v.SeededWhen = seed.Name, seed.By, seed.When
		v.Parent, v.Responsible = seed.Parent, seed.Responsible
		if v.Context, err = ancestors(ctx, cfg, r, dir, scopeID, seed.Parent); err != nil {
			return nil, nil, err
		}
	}
	if v.Parent != "" {
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

// seedTierOf verifies a seed through its envelope, as Describe verifies a claim: against this
// repository's tiers, wherever the seed was read from.
func seedTierOf(ctx context.Context, cfg *config.Config, r index.Reader, dir string, seed *binaries.ScopeSeed) (string, error) {
	envPath := filepath.Join(dir, "seed-"+strings.TrimPrefix(seed.ID, "sha256:")+".dsse.json")
	if err := os.WriteFile(envPath, seed.Envelope, 0o600); err != nil {
		return "", err
	}
	tier, _, err := verify.ResolveTier(ctx, r, cfg, envPath, verify.Claim)
	if err != nil {
		return "", fmt.Errorf("verifying the seed of %s failed: %w", seed.ID, err)
	}
	return tier, nil
}

// ancestors walks the parent chain from parent upward. Each step is a seed read over the union and
// verified here; what an unverified seed says, including its parent, is not followed.
func ancestors(ctx context.Context, cfg *config.Config, r index.Reader, dir, from, parent string) ([]Ancestor, error) {
	var out []Ancestor
	seen := map[string]bool{from: true}
	for parent != "" && !seen[parent] && len(out) < maxContext {
		seen[parent] = true
		seed, err := r.Seed(ctx, parent)
		if err != nil {
			out = append(out, Ancestor{Scope: parent, Missing: true})
			break
		}
		tier, err := seedTierOf(ctx, cfg, r, dir, seed)
		if err != nil {
			return nil, err
		}
		a := Ancestor{Scope: parent, Tier: tier}
		if tier == verify.Untrusted {
			out = append(out, a)
			break
		}
		a.Name, a.SeededBy, a.SeededWhen = seed.Name, seed.By, seed.When
		out = append(out, a)
		parent = seed.Parent
	}
	return out, nil
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
	if v.SeedTier == verify.Untrusted {
		fmt.Fprintf(&b, "seed:    signed by a key no trust tier here holds — its name and parent are not shown\n")
	}
	for _, a := range v.Context {
		switch {
		case a.Missing:
			fmt.Fprintf(&b, "in:      %s  [no source holds its seed]\n", a.Scope)
		case a.Tier == verify.Untrusted:
			fmt.Fprintf(&b, "in:      %s  [seed not trusted here]\n", a.Scope)
		default:
			fmt.Fprintf(&b, "in:      %s (%q)  [%s]\n", a.Scope, a.Name, a.Tier)
		}
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
