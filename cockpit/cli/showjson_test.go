package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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
		if _, trusted := snap.Keys[kid]; trusted {
			t.Errorf("the package's build key %s is in the trusted ring", kid)
		}
	}
}
