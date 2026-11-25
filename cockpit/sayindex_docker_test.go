//go:build docker

package cockpit

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// With the oxigraph index up, a say reads the scope's head from the index's chain graph and chains onto
// the right prev; once the daemon is gone it reads the union and still succeeds.
func TestSay_TheHeadComesFromTheIndexWhileItIsUp(t *testing.T) {
	ours, _, scope := indexedScopedRepo(t)
	cfg := ours.Config(t)
	if _, err := oxigraph.Ensure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	pub := publishOne(t, ours)

	first, trace := sayTraced(t, pub.FotonID)
	if !strings.Contains(trace, "the scope's head (index)") {
		t.Fatalf("want the head read from the index:\n%s", trace)
	}
	if first.Chain.Prev != scope {
		t.Fatalf("the first claim chained onto %s, want the seed %s", first.Chain.Prev, scope)
	}
	second, trace := sayTraced(t, pub.FotonID)
	if !strings.Contains(trace, "the scope's head (index)") {
		t.Fatalf("want the head read from the index:\n%s", trace)
	}
	if second.Chain.Prev != first.ClaimID {
		t.Fatalf("the second claim chained onto %s, want the first, %s", second.Chain.Prev, first.ClaimID)
	}

	if err := oxigraph.Stop(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	third, trace := sayTraced(t, pub.FotonID)
	if !strings.Contains(trace, "(reads the union)") {
		t.Fatalf("the daemon is stopped: want the union read:\n%s", trace)
	}
	if third.Chain.Prev != second.ClaimID {
		t.Fatalf("after the fallback the claim chained onto %s, want %s", third.Chain.Prev, second.ClaimID)
	}
	if tip, length, branched := ours.ScopeHead(t, scope); tip != third.ClaimID || length != 3 || branched {
		t.Fatalf("tip %s length %d branched %v, want %s, 3, not branched", tip, length, branched, third.ClaimID)
	}
}

// kton-review: a teammate's registry (a federation source) gets a claim into the scope after the view
// was built. The next say of ours chains onto it, not onto our own last claim: the head read applied that
// registry's delta first, so the scope does not branch.
func TestSay_ATeammatesClaimSinceTheViewWasBuiltIsTheHeadWeChainOnto(t *testing.T) {
	ours, theirs, scope := indexedScopedRepo(t)
	cfg := ours.Config(t)
	if _, err := oxigraph.Ensure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	pub := publishOne(t, ours)

	first, trace := sayTraced(t, pub.FotonID) // builds the view
	if !strings.Contains(trace, "(index)") {
		t.Fatalf("want the head read from the index:\n%s", trace)
	}
	// The teammate says into the scope, onto our claim, in their registry.
	theirClaim := theirs.ChainClaimDirectly(t, pub.FotonID, scope, first.ClaimID, "theirs")
	// The head read is what a say chains onto (resolveChain); the say itself is not run, because a claim
	// chained onto a record only a source holds waits in our registry and the say's confirmation of it
	// (reads the own registry) says it did not register, with or without the index.
	chain, head, viaIndex, cerr := resolveChain(context.Background(), binaries.New(cfg), cfg, "review")
	if cerr != "" {
		t.Fatal(cerr)
	}
	if !viaIndex {
		t.Fatal("want the head read from the index")
	}
	if chain.Prev != theirClaim || head.Branched || head.ChainLength != 2 || !slices.Equal(head.Heads, []string{theirClaim}) {
		t.Fatalf("chained onto %s (heads %v, length %d, branched %v), want the teammate's %s (ours before it: %s)",
			chain.Prev, head.Heads, head.ChainLength, head.Branched, theirClaim, first.ClaimID)
	}
}

// T2 (kton-review): the daemon is UP but the index cannot answer for the scope (its view holds no resolved
// seed: the configured scope's seed is in no registry of the slot). The index never refuses a say, so the
// say falls back to the union and refuses exactly as the union path does, with the kernel's own error and
// not the index's.
func TestSay_AnIndexErrorWithTheDaemonUpFallsBackToTheUnion(t *testing.T) {
	ours, _, _ := indexedScopedRepo(t)
	cfg := ours.Config(t)
	if _, err := oxigraph.Ensure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), cfg) })
	pub := publishOne(t, ours)

	nobody := "sha256:" + strings.Repeat("ab", 32) // a seed no registry holds
	raw := cfg.Raw
	raw.Claims.Scopes = map[string]string{"review": nobody}
	ours.WriteConfig(t, raw)
	cfg = ours.Config(t)

	// The premise: the daemon is up and the index itself answers with an error.
	d := oxigraph.Up(context.Background(), cfg)
	if d == nil {
		t.Fatal("the index daemon is not up")
	}
	keys, err := trustedKeys(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, ierr := oxigraph.ScopeHead(context.Background(), d, cfg, keys, nobody)
	if ierr == nil {
		t.Fatal("want an index error for a scope whose seed no registry holds")
	}

	var buf bytes.Buffer
	was := traceOut
	traceOut = &buf
	t.Setenv("COCKPIT_TRACE", "1")
	defer func() { traceOut = was }()
	res, _, err := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: "working-on", Scope: "review",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("want the say refused: the scope's seed is in no registry")
	}
	msg := errText(res)
	if !strings.Contains(msg, "cannot report its head") {
		t.Fatalf("want the union path's refusal:\n%s", msg)
	}
	if strings.Contains(msg, ierr.Error()) {
		t.Fatalf("the refusal carries the index's error %q instead of the union's:\n%s", ierr, msg)
	}
	if !strings.Contains(buf.String(), "(reads the union)") {
		t.Fatalf("want the head read from the union after the index error:\n%s", buf.String())
	}
}
