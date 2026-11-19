package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"kton.dev/plankton/core"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// show --json prints the snapshot a viewer fetches (union, keys, names) as one JSON document on
// stdout, so a shell reads it through the command line instead of starting a server
// (userinterface's graph@1; SPEC §12.3).
func TestShowJSON_PrintsTheSnapshotAViewerFetches(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "data/in.csv", "a\n1\n")
	r.Write(t, "work/out.csv", "a\n2\n")
	pubOut, _, code := r.CockpitSplit(t, "publish", "--json", "--in", "data/in.csv", "--out", "work/out.csv", "--", "double", "data/in.csv")
	var pub struct {
		FotonID string `json:"fotonId"`
	}
	if code != 0 || json.Unmarshal([]byte(pubOut), &pub) != nil || pub.FotonID == "" {
		t.Fatalf("publish (exit %d):\n%s", code, pubOut)
	}
	stdout, stderr, code := r.CockpitSplit(t, "show", "--json")
	var snap struct {
		Union []map[string]any `json:"union"`
		Keys  map[string]any   `json:"keys"`
		Names map[string]any   `json:"names"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &snap) != nil {
		t.Fatalf("show --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	if len(snap.Union) == 0 || snap.Keys == nil || snap.Names == nil {
		t.Fatalf("want the union with the record, keys and names: %s", stdout)
	}
	if !strings.Contains(stdout, strings.TrimPrefix(pub.FotonID, "sha256:")) {
		t.Errorf("the published record %s is not in the union", pub.FotonID)
	}
}

// An admitted package's build key is labelled under packages, never put in the trusted ring: a
// record it signed shows as an admitted package's, not as a stranger's or a trusted one's
// (userinterface's graph@1; ADR-016 L2).
func TestShowJSON_LabelsAnAdmittedPackagesKeyOutsideTheRing(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "words"), "words", "v1", key, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	stdout, stderr, code := r.CockpitSplit(t, "show", "--json")
	var snap struct {
		Keys     map[string]any `json:"keys"`
		Packages map[string]struct {
			Package string `json:"package"`
			Key     string `json:"key"`
		} `json:"packages"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &snap) != nil {
		t.Fatalf("show --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	if len(snap.Packages) != 1 {
		t.Fatalf("want the one admitted package's key labelled: %s", stdout)
	}
	for kid, p := range snap.Packages {
		if p.Package != "words" {
			t.Errorf("labelled as %q, want words", p.Package)
		}
		// The key comes along, so a viewer can re-check the signatures; it derives the keyid.
		if pub, err := core.ParsePublicKeyHex(p.Key); err != nil || core.KeyIDHex(pub) != kid {
			t.Errorf("the entry's key %q does not derive its keyid %s: %v", p.Key, kid, err)
		}
		if _, trusted := snap.Keys[kid]; trusted {
			t.Errorf("the package's build key %s is in the trusted ring", kid)
		}
	}
}

// The label is the key the package's seal verifies under, never the keyid it declares, and never a
// key of the trusted ring (kton-review K1 on #167): a package whose seal is relabelled to a ring key's
// keyid, with that key dropped into its keys/, is still labelled by its real build key only; a package
// actually signed by a ring key gets no label, and doctor names the collision.
func TestShowJSON_LabelsOnlyTheVerifyingKeyAndNeverARingKey(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ringPub := strings.TrimSpace(readFile(t, filepath.Join(r.Root, ".kton", "keys", "self", testrepo.SessionID+"-claims.pub")))
	ringKID := keyIDOfHex(t, ringPub)

	_, key, _ := ed25519.GenerateKey(rand.Reader)
	buildKID := core.KeyIDHex(key.Public().(ed25519.PublicKey))
	buildPkg(t, filepath.Join(t.TempDir(), "words"), "words", "v1", key, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	inst := filepath.Join(r.Root, ".kton", "packages", "words@v1")
	os.WriteFile(filepath.Join(inst, "keys", ringKID+".pub"), []byte(ringPub), 0o644)
	files, _ := filepath.Glob(filepath.Join(inst, "nekton", "objects", "*", "*"))
	more, _ := filepath.Glob(filepath.Join(inst, "nekton", "objects", "*"))
	relabelled := 0
	for _, f := range append(files, more...) {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), `"keyid":"`+buildKID+`"`) {
			os.WriteFile(f, []byte(strings.ReplaceAll(string(b), `"keyid":"`+buildKID+`"`, `"keyid":"`+ringKID+`"`)), 0o644)
			relabelled++
		}
	}
	if relabelled == 0 {
		t.Fatal("found no seal envelope to relabel")
	}
	labels := func() map[string]string {
		t.Helper()
		stdout, stderr, code := r.CockpitSplit(t, "show", "--json")
		var snap struct {
			Packages map[string]struct {
				Package string `json:"package"`
			} `json:"packages"`
		}
		if code != 0 || json.Unmarshal([]byte(stdout), &snap) != nil {
			t.Fatalf("show --json (exit %d):\n%s\n%s", code, stdout, stderr)
		}
		out := map[string]string{}
		for k, p := range snap.Packages {
			out[k] = p.Package
		}
		return out
	}
	got := labels()
	if _, bad := got[ringKID]; bad {
		t.Fatalf("a ring key is labelled as a package's: %v", got)
	}
	if got[buildKID] != "words" {
		t.Errorf("the real build key is not labelled: %v", got)
	}

	// A package actually signed by a ring key: no label, and doctor names it.
	ringPriv := loadPriv(t, r.Key(testrepo.SessionID+"-claims"))
	buildPkg(t, filepath.Join(t.TempDir(), "mine"), "mine", "v1", ringPriv, map[string]string{"memo": tmpl("memo", "https://example.org/memo")})
	if _, bad := labels()[ringKID]; bad {
		t.Error("a package signed by a ring key is labelled")
	}
	if out, _ := r.Cockpit(t, "doctor"); !strings.Contains(out, "the admitted package mine is signed by "+ringKID) {
		t.Errorf("doctor does not name the collision:\n%s", out)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func keyIDOfHex(t *testing.T, h string) string {
	t.Helper()
	pub, err := core.ParsePublicKeyHex(h)
	if err != nil {
		t.Fatal(err)
	}
	return core.KeyIDHex(pub)
}

func loadPriv(t *testing.T, path string) ed25519.PrivateKey {
	t.Helper()
	seed, err := hex.DecodeString(strings.TrimSpace(readFile(t, path)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("%s is not a seed", path)
	}
	return ed25519.NewKeyFromSeed(seed)
}
