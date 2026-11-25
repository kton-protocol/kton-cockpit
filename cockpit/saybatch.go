package cockpit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/anchor"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"github.com/kton-protocol/kton-cockpit/internal/show"
)

// SayBatchRequest is many claims said in one write (SPEC §8.9). Each is a SayRequest, with the same
// fields and the same checks as one say.
type SayBatchRequest struct {
	Claims []SayRequest `json:"claims" jsonschema:"the claims, in order; each is what say takes"`
}

// SayBatchResult answers a batch: the claims in the order asked.
type SayBatchResult struct {
	Claims []SayResult `json:"claims"`
	// Pending is set when the claims are signed and stored but their commit met the repository's lock
	// (store.busy): they exist, and the next write commits them. A caller must not say them again.
	Pending *Pending `json:"pending,omitempty"`
}

// maxBatch bounds one batch, so a mistaken caller cannot hold the claims lock without end.
const maxBatch = 100000

// SayBatch says many claims in one write: every claim is checked before any is signed, all are signed
// with the template set and keys read once, stored through one open of the store, committed once and
// confirmed once. Claims into one scope chain one after another in the order given, and replace:
// previous sees the earlier claims of the batch. A refusal names the claim (by its position, from 1)
// and nothing is written.
func (c *Cockpit) SayBatch(ctx context.Context, in SayBatchRequest) (*SayBatchResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if !cfg.Raw.Verbs.Say {
		return nil, refuse("verb.disabled", "SPEC §6", "say is disabled by this repo's cockpit.config.json")
	}
	switch {
	case len(in.Claims) == 0:
		return nil, refuse("argument", "SPEC §8.9", "a batch needs at least one claim")
	case len(in.Claims) > maxBatch:
		return nil, refuse("argument", "SPEC §8.9", "a batch holds at most %d claims, this one %d: split it", maxBatch, len(in.Claims))
	}
	r := binaries.New(cfg)
	at := func(i int, err error) error {
		var ref *Refusal
		if errors.As(err, &ref) {
			return &Refusal{Code: ref.Code, Clause: ref.Clause, Reason: fmt.Sprintf("claim %d: %s", i+1, ref.Reason)}
		}
		return refuse("kernel", "", "claim %d: %v", i+1, err)
	}

	// Every claim checked before anything is signed or read under the lock.
	preps := make([]*preparedSay, len(in.Claims))
	for i, one := range in.Claims {
		if one.Template == "reproduces" {
			return nil, at(i, refuse("argument", "SPEC §8.9", "reproduces runs a reproduction per claim and is said one at a time"))
		}
		p, err := prepareSay(ctx, cfg, r, one)
		if err != nil {
			return nil, at(i, err)
		}
		preps[i] = p
	}
	signer, err := r.NewClaimSigner()
	if err != nil {
		return nil, refuse("vocabulary", "SPEC §8.1", "%v", err)
	}

	unlock, err := r.LockClaims()
	if err != nil {
		return nil, refuse("store", "", "%v", err)
	}
	locked := true
	release := func() {
		if locked {
			locked = false
			unlock()
		}
	}
	defer release()

	// Each scope's head is read once; within the batch a scope's next claim chains onto the one before.
	type scopeState struct {
		chain  *binaries.Chain
		head   *binaries.ScopeHead
		said   bool // a claim of the batch went into it already
		length int  // the chain's length with the batch's claims so far
	}
	scopes := map[string]*scopeState{}
	// The newest claim of the batch per (template, subject, signer), for replace: previous.
	ownInBatch := map[[3]string]string{}
	envs := make([]core.Envelope, len(preps))
	out := &SayBatchResult{Claims: make([]SayResult, len(preps))}
	for i, p := range preps {
		var chain *binaries.Chain
		if p.in.Scope != "" {
			st := scopes[p.in.Scope]
			if st == nil {
				ch, head, _, cerr := resolveChain(ctx, r, cfg, p.in.Scope)
				if cerr != "" {
					return nil, at(i, refuse("scope.unknown", "", "%s", cerr))
				}
				st = &scopeState{chain: ch, head: head, length: head.ChainLength}
				scopes[p.in.Scope] = st
			}
			if p.in.ExpectHead != "" {
				want, ok := core.NormalizeContentHash(p.in.ExpectHead)
				// The first claim of a scope in the batch names a head the scope has; each later one names the
				// claim of the batch before it, which is then the scope's only head.
				switch {
				case !ok:
					return nil, at(i, refuse("argument", "SPEC §8.5", "expectHead is a claim id (sha256:<hex>), not %q", p.in.ExpectHead))
				case !st.said && !slices.Contains(st.head.Heads, want):
					return nil, at(i, refuse("scope.moved", "SPEC §8.5", "the head of scope %s is no longer %s but %s: read what was written since, then say again",
						p.in.Scope, want, strings.Join(st.head.Heads, ", ")))
				case st.said && want != st.chain.Prev:
					return nil, at(i, refuse("scope.moved", "SPEC §8.5", "in this batch the head of scope %s is %s, the claim before, not %s",
						p.in.Scope, st.chain.Prev, want))
				}
				st.chain.Prev = want
			}
			chain = &binaries.Chain{Scope: st.chain.Scope, Prev: st.chain.Prev}
		} else if p.in.ExpectHead != "" {
			return nil, at(i, refuse("argument", "SPEC §8.5", "expectHead needs scope: it is a head of the scope the claim joins"))
		}

		sets := p.sets
		key := [3]string{p.in.Template, p.in.Subject, p.signKey}
		if sets["replaces"] == "" && signer.ReplacesPrevious(p.in.Template) {
			prev := ownInBatch[key]
			if prev == "" {
				if prev, err = previousOwn(ctx, cfg, r, p.in.Template, p.in.Subject, p.signKey); err != nil {
					return nil, at(i, refuse("kernel", "", "finding the claim this one replaces: %v", err))
				}
			}
			if prev != "" {
				sets = cloneSets(sets)
				sets["replaces"] = prev
				out.Claims[i].Replaces = prev
			}
		}
		env, id, err := signer.Sign(ctx, p.in.Subject, p.in.Template, sets, p.signKey, chain, p.expect)
		var changed *binaries.TemplateChanged
		if errors.As(err, &changed) {
			return nil, at(i, refuse("template.changed", "SPEC §8", "%s", changed.Reason))
		} else if err != nil {
			return nil, at(i, err)
		}
		envs[i] = env
		ownInBatch[key] = id
		if chain != nil {
			st := scopes[p.in.Scope]
			// Length is how many claims the scope held before this one, as say answers it.
			out.Claims[i].Chain = &ChainPosition{Scope: chain.Scope, Prev: chain.Prev, Length: st.length}
			st.chain.Prev, st.said = id, true
			st.length++
		}
		out.Claims[i].ClaimID, out.Claims[i].Subject, out.Claims[i].ResolvedFrom = id, p.in.Subject, p.resolvedFrom
	}

	// Nothing is stored before this point. The store is append-only: a refusal from here on leaves the
	// claims before it stored, and says how many.
	n, err := r.AddClaims(envs)
	release()
	var refused *binaries.AddRefused
	if errors.As(err, &refused) {
		scopeOf := make([]string, len(preps))
		for k, p := range preps {
			scopeOf[k] = p.in.Scope
		}
		return nil, batchRefusal(refused, scopeOf)
	} else if err != nil {
		return nil, refuse("kernel", "", "%v (%d claim(s) are stored, not committed)", err, n)
	}
	// Each claim carries what one say's claim carries: the configured evidence, and its anchor when the
	// repository anchors (kton-review A1 on #188), so no form of say is laxer than another (SPEC §6).
	for i := range out.Claims {
		if err := material.Attach(ctx, cfg, r, out.Claims[i].ClaimID, material.Claim); err != nil {
			return nil, at(i, refuse("material", "SPEC §11", "%v", err))
		}
		if cfg.Raw.Anchor.Enabled {
			e, err := anchor.Record(ctx, cfg, out.Claims[i].ClaimID, anchor.Claim)
			if err != nil {
				return nil, at(i, refuse("anchor", "", "the claim was registered but anchoring it failed: %v", err))
			}
			out.Claims[i].RekorLogIndex, out.Claims[i].RekorUUID = e.LogIndex, e.UUID
		}
	}

	registryPaths := []string{config.Rel("nekton")}
	// Each claim's files kept with it, as say keeps one's (B11).
	if set, err := r.Templates(); err == nil {
		anyShared := false
		for i, p := range preps {
			var paths []string
			want := map[string]string{}
			if p.resolvedFrom != "" {
				paths = append(paths, p.resolvedFrom)
				want[p.resolvedFrom] = p.in.Subject
			}
			paths = append(paths, fileFieldPaths(set, p.in.Template, p.sets)...)
			kept, shared := keepSigned(ctx, cfg, paths, want, signedHashes(cfg, out.Claims[i].ClaimID))
			out.Claims[i].Kept = kept
			anyShared = anyShared || shared
		}
		if anyShared {
			registryPaths = append(registryPaths, config.Rel("blobs"))
		}
	}
	if cfg.Raw.Union.Publish {
		written, uerr := show.WriteUnion(ctx, cfg)
		if uerr != nil {
			return nil, refuse("union", "", "the claims were registered but publishing the union failed: %v", uerr)
		}
		registryPaths = append(registryPaths, written...)
	}
	_, perr := persistStrict(ctx, cfg, registryPaths, fmt.Sprintf("claims: %d said in one batch", len(envs)))
	pending, err := pendingIfBusy(perr, fmt.Sprintf("the %d claims", len(envs)))
	if err != nil {
		return nil, refuse(storeCode(err), "", "git commit/push of the claim registry failed: %v", err)
	}
	out.Pending = pending

	// Confirmed by querying them back, once per subject, as say does for one: in this repository's own
	// registry, where they were written, opened once (T12).
	own, err := binaries.OpenNektonSource(cfg.NektonDir)
	if err != nil {
		return nil, refuse("kernel", "", "the claims were recorded but this repository's claim registry cannot be read back: %v", err)
	}
	bySubject := map[string]map[string]bool{}
	waiting := map[int]string{} // claims the own registry holds deferred, by position, and the scope each waits in
	var waitingIDs []string
	for i := range out.Claims {
		subj := out.Claims[i].Subject
		if bySubject[subj] == nil {
			claims, err := binaries.ClaimsAbout(own, subj)
			if err != nil {
				return nil, refuse("kernel", "", "the claims were recorded but the confirmation query (nekton about) failed: %v", err)
			}
			bySubject[subj] = map[string]bool{}
			for _, cl := range claims {
				bySubject[subj][cl.ID] = true
			}
		}
		if bySubject[subj][out.Claims[i].ClaimID] {
			out.Claims[i].Confirmation = "registered: nekton reports " + out.Claims[i].ClaimID + " about " + subj
			continue
		}
		// A claim chained onto a predecessor only a federation source holds is held deferred by the own
		// registry, which waits for it (the same case as one say, SPEC §8.5): the union decides, below.
		if _, scope, ok := own.DeferredClaim(out.Claims[i].ClaimID); ok {
			waiting[i] = scope
			waitingIDs = append(waitingIDs, out.Claims[i].ClaimID)
			continue
		}
		return nil, refuse("claim.not-registered", "", "claim %d (%s) was signed but nekton does not report it about %s, and the registry does not hold it deferred either — it did not register",
			i+1, out.Claims[i].ClaimID, subj)
	}
	if len(waitingIDs) > 0 {
		// The union is opened once for the whole batch, and only when some claim waits.
		held, uerr := binaries.New(cfg).UnionResolves(ctx, waitingIDs...)
		for i := range out.Claims {
			scope, ok := waiting[i]
			if !ok {
				continue
			}
			id := out.Claims[i].ClaimID
			if uerr != nil || !held[id] {
				why := "no registry of the federation holds it"
				if uerr != nil {
					why = fmt.Sprintf("the federation could not be read: %v", uerr)
				}
				return nil, refuse("claim.not-registered", "", "claim %d (%s) was signed and is held in this repository's registry, but it waits for its predecessor in scope %s and %s — it is not registered",
					i+1, id, scope, why)
			}
			out.Claims[i].WaitingOn = scope
			out.Claims[i].Confirmation = fmt.Sprintf("registered: %s is held in this repository's registry waiting for its predecessor in scope %s, which the federation holds, and the union of the registries resolves it", id, scope)
		}
	}
	return out, nil
}

func cloneSets(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// batchRefusal says what a substrate refusal while storing a batch left behind. A claim of a scope
// after a refused one of that scope is stored but chains onto a claim the store does not hold, so it
// hangs: saying the refused claim again makes a new claim, which it does not chain onto. So the answer
// names the refused claims, the ones left hanging, and the ones stored and whole, and says to say the
// refused and the hanging ones again as a new batch, from the scope's current head (kton-review C1 on
// #189).
func batchRefusal(refused *binaries.AddRefused, scopeOf []string) error {
	isRefused := map[int]bool{}
	for _, x := range refused.Refused {
		isRefused[x.Index] = true
	}
	broken := map[string]bool{} // scopes with a refused claim so far
	var bad, hanging []string
	whole := 0
	for i, scope := range scopeOf {
		switch {
		case isRefused[i]:
			bad = append(bad, fmt.Sprint(i+1))
			if scope != "" {
				broken[scope] = true
			}
		case scope != "" && broken[scope]:
			hanging = append(hanging, fmt.Sprint(i+1))
		default:
			whole++
		}
	}
	reason := fmt.Sprintf("the store refused claim(s) %s, the first: %v", strings.Join(bad, ", "), refused.Refused[0].Err)
	if len(hanging) > 0 {
		reason += fmt.Sprintf("; claim(s) %s are stored but chain onto a refused claim and hang", strings.Join(hanging, ", "))
	}
	reason += fmt.Sprintf("; %d claim(s) are stored and whole, not committed yet (the next write commits them)", whole)
	if len(hanging) > 0 {
		reason += ". Say the refused and the hanging claims again, as a new batch from the scope's current head"
	} else {
		reason += ". Say the refused claims again"
	}
	return refuse("kernel", "SPEC §8.9", "%s", reason)
}
