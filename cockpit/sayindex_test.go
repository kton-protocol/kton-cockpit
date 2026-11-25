package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	nregistry "kton.dev/nekton/registry"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// sayTraced says one claim into the scope "review" and returns it with the trace of the head read.
func sayTraced(t *testing.T, subject string) (SayResult, string) {
	t.Helper()
	var buf bytes.Buffer
	was := traceOut
	traceOut = &buf
	t.Setenv("COCKPIT_TRACE", "1")
	defer func() { traceOut = was }()
	res, out, err := Say(context.Background(), nil, SayRequest{Subject: subject, Template: "working-on", Scope: "review",
		Fields: map[string]string{"step": "analysis", "by-session": testrepo.SessionID}})
	if err != nil || res.IsError {
		t.Fatalf("say: %v %s", err, errText(res))
	}
	return out, buf.String()
}

// indexedScopedRepo is a repository whose config names an oxigraph index and the scope "review", seeded
// in both it and a peer's repository, which the config names as a source. No daemon is started.
func indexedScopedRepo(t *testing.T) (ours, theirs *testrepo.Repo, scope string) {
	t.Helper()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	theirs, ours = testrepo.New(t), testrepo.New(t)
	scope = ours.SeedScope(t, "review-2026-09")
	// The seed reaches the teammate's registry too, as a scope's seed does in a federation.
	from, err := nregistry.Open(filepath.Join(ours.Root, config.Dir, "nekton"))
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := from.Claim(scope)
	if !ok {
		t.Fatal("the seed is not in our registry")
	}
	to, err := nregistry.Open(filepath.Join(theirs.Root, config.Dir, "nekton"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := to.Add(rec.Envelope); err != nil {
		t.Fatal(err)
	}
	raw := testrepo.DefaultConfig()
	raw.Claims.Scopes = map[string]string{"review": scope}
	raw.Query.Image = "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"
	raw.Index.Kind = "oxigraph"
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	raw.Federation.Sources = []source.Spec{spec}
	ours.WriteConfig(t, raw)
	ours.Use(t)
	return ours, theirs, scope
}

// With an oxigraph index configured and no daemon up, a say reads the scope's head from the union, as it
// did before the index, and succeeds: the index never refuses a say and never starts a daemon for one.
func TestSay_WithoutAReachableIndexTheHeadComesFromTheUnion(t *testing.T) {
	ours, _, scope := indexedScopedRepo(t)
	pub := publishOne(t, ours)
	first, trace := sayTraced(t, pub.FotonID)
	if !strings.Contains(trace, "the scope's head (reads the union)") || strings.Contains(trace, "(index)") {
		t.Fatalf("no daemon: want the head read from the union:\n%s", trace)
	}
	second, _ := sayTraced(t, pub.FotonID)
	if second.Chain.Prev != first.ClaimID {
		t.Fatalf("the second claim chained onto %s, want the first, %s", second.Chain.Prev, first.ClaimID)
	}
	if tip, length, branched := ours.ScopeHead(t, scope); tip != second.ClaimID || length != 2 || branched {
		t.Fatalf("tip %s length %d branched %v, want %s, 2, not branched", tip, length, branched, second.ClaimID)
	}
}
