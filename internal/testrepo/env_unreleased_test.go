//go:build unreleased

package testrepo

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// With an environment class the record covers the class, and the image a producer ran in is its
// own signed account beside the record (ADR-010, kton-env/v1). Runs in two images of one class are
// one record, co-signed; each producer's account is read only under a key the reader trusts.
func TestAuthor_WithAClassTheImageIsCarriedAndTwoImagesAreOneRecord(t *testing.T) {
	ctx := context.Background()
	alice, bob := New(t), New(t)
	ac := alice.Config(t)
	in := work(t, alice)
	in.Environment, in.EnvRef = class, img1
	first, err := binaries.New(ac).Author(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	f, err := binaries.New(ac).Foton(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := f.Protocol.Descriptor["envRef"]; has || f.Protocol.Descriptor["environment"] != class {
		t.Fatalf("with a class the record covers the class and not the image: %v", f.Protocol.Descriptor)
	}

	// bob ran the same computation in another image of the class, into alice's registry.
	in.EnvRef, in.SignKey = img2, bob.Config(t).PlanktonKey
	second, err := binaries.New(ac).Author(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.CoSigned {
		t.Fatalf("two images of one class gave %s and %s (co-signed %v), want one record", first.ID, second.ID, second.CoSigned)
	}

	d, err := binaries.New(ac).FotonByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.RanIn) != 1 || d.RanIn[0].EnvRef != img1 || d.EnvRef != img1 {
		t.Fatalf("alice trusts only herself, so only her account counts: %+v", d)
	}

	ac.Raw.Trust.Tiers["bob"] = []string{filepath.Join(bob.Root, "registry", "keys", SessionID+".pub")}
	d, err = binaries.New(ac).FotonByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ri := range d.RanIn {
		got[ri.EnvRef] = true
	}
	if len(d.RanIn) != 2 || !got[img1] || !got[img2] {
		t.Fatalf("trusting bob, both accounts count: %+v", d.RanIn)
	}
	own := ""
	for _, ri := range d.RanIn {
		if ri.Signer == d.SignerKeyID {
			own = ri.EnvRef
		}
	}
	if d.EnvRef != own {
		t.Fatalf("envRef is not the account of the signer it reports (or empty): %+v", d)
	}
}
