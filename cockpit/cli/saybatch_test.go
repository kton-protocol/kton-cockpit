package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// say --batch reads one say request per line and answers one JSON document: the claims in order, or a
// refusal naming the line or the claim, with nothing said (SPEC §8.9).
func TestShellSay_BatchFromAFile(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	file := filepath.Join(t.TempDir(), "claims.jsonl")
	line := func(step string) string {
		return `{"template": "working-on", "subject": "urn:example:item", "fields": {"step": "` + step + `", "by-session": "s"}}`
	}
	os.WriteFile(file, []byte(line("one")+"\n\n"+line("two")+"\n"+line("three")+"\n"), 0o644)
	stdout, stderr, code := r.CockpitSplit(t, "say", "--batch", file)
	var out struct {
		Claims []struct {
			ClaimID string `json:"claimId"`
		} `json:"claims"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &out) != nil || len(out.Claims) != 3 {
		t.Fatalf("say --batch (exit %d): %s\n%s", code, stdout, stderr)
	}
	for _, c := range out.Claims {
		if !strings.HasPrefix(c.ClaimID, "sha256:") {
			t.Errorf("a claim without an id: %s", stdout)
		}
	}
	if stdout, _, code := r.CockpitSplit(t, "say", "--batch", file, "--json"); code != 0 || !json.Valid([]byte(stdout)) {
		t.Errorf("say --batch FILE --json: want taken, as say's other forms take --json (exit %d): %s", code, stdout)
	}

	os.WriteFile(file, []byte(line("one")+"\n"+`{"template": "working-on", "nonsense": 1}`+"\n"), 0o644)
	stdout, _, code = r.CockpitSplit(t, "say", "--batch", file)
	var ref struct{ Code, Reason string }
	if code != 2 || json.Unmarshal([]byte(stdout), &ref) != nil || ref.Code != "argument" || !strings.Contains(ref.Reason, "line 2") {
		t.Errorf("a line that is no say request: want refused naming line 2, got exit %d %s", code, stdout)
	}
	os.WriteFile(file, []byte(line("one")+"\n"+`{"template": "not-allowed", "subject": "urn:example:item"}`+"\n"), 0o644)
	stdout, _, code = r.CockpitSplit(t, "say", "--batch", file)
	if code != 2 || json.Unmarshal([]byte(stdout), &ref) != nil || ref.Code != "template.not-allowed" || !strings.HasPrefix(ref.Reason, "claim 2: ") {
		t.Errorf("a claim outside the ceiling: want refused naming claim 2, got exit %d %s", code, stdout)
	}
}
