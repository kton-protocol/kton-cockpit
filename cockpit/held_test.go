package cockpit

import (
	"context"
	"testing"
)

// A Cockpit from Open answers as one from New does, keeping the opened union between calls until a
// registry changes; Close lets it go.
func TestHeld_KeepsTheUnionUntilARegistryChanges(t *testing.T) {
	ctx := context.Background()
	r := describesRepo(t)
	f := publishOne(t, r).FotonID
	c, err := Open(ctx, Start{})
	if err != nil {
		t.Fatal(err)
	}
	ask := func() *AskResult {
		t.Helper()
		out, err := c.Ask(ctx, AskRequest{Query: "about", Ref: f})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if a := ask(); a.Index == nil || a.Index.Kept {
		t.Fatalf("first call: want the union opened and held: %+v", a.Index)
	}
	if a := ask(); a.Index == nil || !a.Index.Kept {
		t.Fatalf("nothing changed: want the held union: %+v", a.Index)
	}
	sayValue(t, f, "describes", "a new claim")
	if a := ask(); a.Index.Kept || len(a.Claims) != 1 {
		t.Fatalf("a new claim: want the union opened again, with the claim: %+v %d", a.Index, len(a.Claims))
	}
	c.Close()
	if a := ask(); a.Index.Kept {
		t.Fatalf("after Close: want nothing held: %+v", a.Index)
	}

	if out, err := New(Start{}).Ask(ctx, AskRequest{Query: "about", Ref: f}); err != nil || out.Index != nil {
		t.Errorf("New with index.kind none: want the registries opened for the call, nothing held: %+v %v", out.Index, err)
	}
	t.Setenv("COCKPIT_REPO_DIR", t.TempDir())
	if _, err := Open(ctx, Start{}); err == nil {
		t.Error("Open outside a repository: want the binding refused")
	}
}
