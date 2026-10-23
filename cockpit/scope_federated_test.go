package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// federatedScopes seeds programme → project → study in a peer's repository, and returns a repo of
// ours that names the peer as a dir source, trusting its claims key when trust is set.
func federatedScopes(t *testing.T, trust bool) (study, project, programme string) {
	t.Helper()
	theirs := testrepo.New(t)
	programme = theirs.SeedScope(t, "programme-cardio")
	project = theirs.SeedScopeUnder(t, "project-af", programme)
	study = theirs.SeedScopeUnder(t, "study-af-2026", project)

	ours := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	raw.Federation.Sources = []source.Spec{spec}
	if trust {
		pub, err := os.ReadFile(filepath.Join(theirs.Root, "registry", "keys", testrepo.SessionID+"-claims.pub"))
		if err != nil {
			t.Fatal(err)
		}
		ours.Write(t, "registry/keys/theirs-claims.pub", string(pub))
		raw.Trust.Tiers["peer"] = []string{"registry/keys/theirs-claims.pub"}
	}
	ours.WriteConfig(t, raw)
	ours.Use(t)
	return study, project, programme
}

// A scope seeded in a federated source is found, verified against this repository's tiers, and
// answered with its parents in one go — so a caller does not ask every source in turn.
func TestScope_ASeedFromASourceIsVerifiedAndCarriesItsContext(t *testing.T) {
	study, project, programme := federatedScopes(t, true)

	res, out, err := Ask(context.Background(), nil, AskRequest{Query: "scope", Ref: study})
	if err != nil || res.IsError {
		t.Fatalf("ask scope on a source's seed: err=%v %s", err, errText(res))
	}
	v := out.Seal
	if v.SeedTier != "peer" || v.Name != "study-af-2026" || v.Parent != project {
		t.Fatalf("want the study verified in tier peer under its project, got %+v", v)
	}
	if len(v.Context) != 2 || v.Context[0].Scope != project || v.Context[0].Name != "project-af" ||
		v.Context[1].Scope != programme || v.Context[1].Name != "programme-cardio" || v.Context[1].Tier != "peer" {
		t.Fatalf("want project then programme, both verified, got %+v", v.Context)
	}
	if !strings.Contains(out.Raw, "programme-cardio") {
		t.Errorf("the text does not show the context:\n%s", out.Raw)
	}
}

// A seed no tier here holds is still found — the chain is a fact about the store — but what it
// says is not shown, and its parent is not followed.
func TestScope_AnUntrustedSeedFromASourceSaysNothingItClaims(t *testing.T) {
	study, _, _ := federatedScopes(t, false)

	res, out, err := Ask(context.Background(), nil, AskRequest{Query: "scope", Ref: study})
	if err != nil || res.IsError {
		t.Fatalf("ask scope on a source's seed: err=%v %s", err, errText(res))
	}
	v := out.Seal
	if v.SeedTier != "" || v.Name != "" || v.SeededBy != "" || v.Parent != "" || len(v.Context) != 0 {
		t.Fatalf("an untrusted seed leaked what it says: %+v", v)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "study-af-2026") || strings.Contains(string(b), "project-af") {
		t.Fatalf("the answer carries an untrusted seed's names: %s", b)
	}
}
