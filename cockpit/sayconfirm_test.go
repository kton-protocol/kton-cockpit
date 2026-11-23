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

// A say is confirmed in this repository's own registry, where it wrote the claim, not by reading the union:
// a federation source that cannot be read does not fail it, and none is fetched for the check (16 of 20 s
// of a say in MH's 4-source registry, userinterface's T12 trace).
func TestSay_ConfirmedInTheOwnRegistry(t *testing.T) {
	ctx := context.Background()
	theirs := testrepo.New(t)
	ours := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	var spec source.Spec
	b, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": theirs.Root})
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	raw.Federation.Sources = []source.Spec{spec}
	ours.WriteConfig(t, raw)
	ours.Use(t)
	// The source loses its registries: any read of the union now fails.
	if err := os.RemoveAll(filepath.Join(theirs.Root, ".kton")); err != nil {
		t.Fatal(err)
	}
	subj := "sha256:" + strings.Repeat("b", 64)
	res, err := New(Start{}).Say(ctx, SayRequest{Subject: subj, Template: "describes", Fields: map[string]string{"value": "v"}})
	if err != nil {
		t.Fatalf("a say with an unreadable source: %v", err)
	}
	if !strings.Contains(res.Confirmation, res.ClaimID) {
		t.Errorf("confirmed in the own registry: %q", res.Confirmation)
	}
	batch, err := New(Start{}).SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{
		{Subject: subj, Template: "describes", Fields: map[string]string{"value": "w"}},
	}})
	if err != nil || len(batch.Claims) != 1 || !strings.Contains(batch.Claims[0].Confirmation, batch.Claims[0].ClaimID) {
		t.Errorf("a batch with an unreadable source: %+v %v", batch, err)
	}
}
