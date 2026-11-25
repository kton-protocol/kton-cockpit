package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// sourceHeldScope seeds a scope in a peer's repository with one claim chained into it, then makes a repository
// of ours that names the peer as a source and the scope "review": the scope's head is the peer's claim, which
// our own registry does not hold, so a claim said here is held deferred in ours.
func sourceHeldScope(t *testing.T) (ours, theirs *testrepo.Repo, scope, head string) {
	t.Helper()
	theirs = testrepo.New(t)
	scope = theirs.SeedScope(t, "review-2026-09")
	head = theirs.ChainClaimDirectly(t, "sha256:"+strings.Repeat("a", 64), scope, scope, "theirs")

	ours = testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = map[string]string{"review": scope}
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	raw.Federation.Sources = []source.Spec{spec}
	ours.WriteConfig(t, raw)
	ours.Use(t)
	return ours, theirs, scope, head
}

// A claim said into a scope whose head a federation source holds is held deferred by the own registry, which
// waits for that head. It was signed and written, so say must confirm it through the union, not refuse it as
// not registered.
func TestSay_ConfirmsAClaimThatChainsOntoAFederationSource(t *testing.T) {
	_, _, scope, head := sourceHeldScope(t)
	subj := "sha256:" + strings.Repeat("b", 64)
	res, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subj, Template: "working-on", Scope: "review",
		Fields: map[string]string{"step": "mine", "by-session": "s"}})
	if err != nil {
		t.Fatalf("a say onto a head only a source holds: %v", err)
	}
	if res.Chain == nil || res.Chain.Prev != head {
		t.Fatalf("want the claim chained onto the source's head %s: %+v", head, res.Chain)
	}
	if res.WaitingOn != scope {
		t.Errorf("waitingOn: want %s, got %q", scope, res.WaitingOn)
	}
	if !strings.Contains(res.Confirmation, "waiting for its predecessor") {
		t.Errorf("the confirmation should say the claim waits: %q", res.Confirmation)
	}
	cfg, lerr := config.Load(context.Background(), "")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, deferred, derr := binaries.New(cfg).OwnDeferred(res.ClaimID); derr != nil || !deferred {
		t.Errorf("the claim should be in the own registry, deferred: %v %v", deferred, derr)
	}
}

// A claim no registry resolves, because its predecessor is held nowhere, is refused with what it waits for.
func TestConfirmSay_RefusesAClaimNoRegistryResolves(t *testing.T) {
	_, theirs, scope, _ := sourceHeldScope(t)
	subj := "sha256:" + strings.Repeat("b", 64)
	res, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subj, Template: "working-on", Scope: "review",
		Fields: map[string]string{"step": "mine", "by-session": "s"}})
	if err != nil {
		t.Fatal(err)
	}
	// The source loses its registry after the head was read: the predecessor is now held nowhere.
	nek := filepath.Join(theirs.Root, ".kton", "nekton")
	if err := os.RemoveAll(nek); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nek, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, lerr := config.Load(context.Background(), "")
	if lerr != nil {
		t.Fatal(lerr)
	}
	_, _, cerr := confirmSay(context.Background(), binaries.New(cfg), res.ClaimID, subj, nil)
	ref, ok := cerr.(*Refusal)
	if !ok || ref.Code != "claim.not-registered" || !strings.Contains(ref.Reason, "waits for its predecessor in scope "+scope) ||
		!strings.Contains(ref.Reason, "no registry of the federation holds it") {
		t.Fatalf("want claim.not-registered naming the wait, got %v", cerr)
	}

	// A claim the registry does not hold at all is refused without a read of the federation.
	_, _, cerr = confirmSay(context.Background(), binaries.New(cfg), "sha256:"+strings.Repeat("c", 64), subj, nil)
	if ref, ok := cerr.(*Refusal); !ok || ref.Code != "claim.not-registered" || !strings.Contains(ref.Reason, "does not hold it deferred") {
		t.Fatalf("want claim.not-registered for a claim not held, got %v", cerr)
	}
}

// An ordinary say is confirmed in the own registry and does not open the federation: the trace has no
// federation phase.
func TestSay_AnOrdinarySayDoesNotReadTheFederation(t *testing.T) {
	sourceHeldScope(t)
	t.Setenv("COCKPIT_TRACE", "1")
	var buf bytes.Buffer
	old := traceOut
	traceOut = &buf
	defer func() { traceOut = old }()
	subj := "sha256:" + strings.Repeat("b", 64)
	if _, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subj, Template: "working-on",
		Fields: map[string]string{"step": "mine", "by-session": "s"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "confirm (reads the own registry)") || strings.Contains(buf.String(), "confirm (reads the federation)") {
		t.Errorf("an ordinary say reads the own registry only:\n%s", buf.String())
	}
}
