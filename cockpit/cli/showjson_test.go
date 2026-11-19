package cli

import (
	"encoding/json"
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
