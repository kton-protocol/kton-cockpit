//go:build docker

package oxigraph_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/core"
	pregistry "kton.dev/plankton/registry"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// Attribution belongs to the registry that holds the signature. bob pulled alice's record before
// carol co-signed it in alice's registry, so his copy carries no signature of carol's: his answer
// must not attribute the record to her, whatever another query read from alice's registry.
// (Found by kton-review on PR #8.)
func TestOxigraph_AttributionIsTheRegistrysOwn(t *testing.T) {
	ctx := context.Background()
	t.Setenv("COCKPIT_INDEX_DIR", t.TempDir())
	alice := testrepo.New(t)
	ac := indexed(t, alice)
	d, err := oxigraph.Ensure(ctx, ac)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oxigraph.Stop(context.Background(), ac) })
	f := author(t, alice, ac, "a")

	// bob pulls (a plain copy of the object files, as git would bring them)
	bob := testrepo.New(t)
	bc := indexed(t, bob)
	if out, err := exec.Command("cp", "-rn", filepath.Join(alice.Root, "registry", "plankton", "objects")+"/.",
		filepath.Join(bob.Root, "registry", "plankton", "objects")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	// carol co-signs f in alice's registry afterwards
	cpub, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	ra, err := pregistry.Open(filepath.Join(alice.Root, "registry", "plankton"))
	if err != nil {
		t.Fatal(err)
	}
	env, ok := ra.Envelope(f)
	if !ok {
		t.Fatal("no envelope")
	}
	pl, _ := env.PayloadBytes()
	sig := ed25519.Sign(cpriv, core.PAE(env.PayloadType, pl))
	co := env
	co.Signatures = append(env.Signatures[:0:0], env.Signatures...)
	co.Signatures = append(co.Signatures, struct {
		KeyID string `json:"keyid"`
		Sig   string `json:"sig"`
	}{core.KeyIDHex(cpub), base64.StdEncoding.EncodeToString(sig)})
	if _, _, err := ra.Add(co); err != nil {
		t.Fatal(err)
	}

	carolID := core.KeyIDHex(cpub)
	// someone trusting carol reads alice's registry
	if _, err := oxigraph.Query(ctx, d, ac, append(keys(t, ac), cpub), attributed); err != nil {
		t.Fatal(err)
	}

	// bob trusts carol, reads only his own registry
	bk := append(keys(t, bc), cpub)
	q := `PREFIX prov: <http://www.w3.org/ns/prov#> SELECT ?a ?k WHERE { ?a prov:wasAttributedTo ?k }`
	a, err := oxigraph.Query(ctx, d, bc, bk, q)
	if err != nil {
		t.Fatal(err)
	}
	daemonSaysCarol := false
	for _, r := range a.Rows {
		if strings.Contains(r["k"], carolID) {
			daemonSaysCarol = true
		}
	}

	// kernel truth for bob's registry
	rb, err := pregistry.Open(filepath.Join(bob.Root, "registry", "plankton"))
	if err != nil {
		t.Fatal(err)
	}
	benv, _ := rb.Envelope(f)
	kernelCarol := false
	if ok, _ := benv.Verify(cpub); ok {
		kernelCarol = true
	}

	// the per-query container's dataset for bob's union
	u, err := binaries.OpenUnion(ctx, bc)
	if err != nil {
		t.Fatal(err)
	}
	def, _, err := oxigraph.Dataset(u, bk)
	if err != nil {
		t.Fatal(err)
	}
	containerCarol := bytes.Contains(def, []byte(carolID))

	if kernelCarol || containerCarol {
		t.Fatalf("the setup is wrong: bob's copy verifies under carol (%v), or his dataset names her (%v)", kernelCarol, containerCarol)
	}
	if daemonSaysCarol {
		t.Errorf("the daemon attributes %s to carol in bob's answer, but bob's registry holds no signature of hers", f)
	}
}
