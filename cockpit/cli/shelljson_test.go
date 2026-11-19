package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A shell that drives the cockpit reads stdout as one JSON document: every listing and status it
// calls has a --json form, and under --json a refusal or an error is {code, clause, reason} on
// stdout too, with exit 2 for a refusal and 1 for anything else (SPEC §6).
func TestShellJSON_EveryListingAndRefusalIsOneDocument(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "work/out.csv", "a\n2\n")

	parse := func(args ...string) (any, int) {
		t.Helper()
		stdout, stderr, code := r.CockpitSplit(t, args...)
		var v any
		if err := json.Unmarshal([]byte(stdout), &v); err != nil {
			t.Fatalf("cockpit %s: stdout is not one JSON document (exit %d): %v\n%s\nstderr:\n%s",
				strings.Join(args, " "), code, err, stdout, stderr)
		}
		return v, code
	}
	ok := func(args ...string) any {
		t.Helper()
		v, code := parse(args...)
		if code != 0 {
			t.Fatalf("cockpit %s exited %d: %v", strings.Join(args, " "), code, v)
		}
		return v
	}

	if v := ok("version", "--json").(map[string]any); v["cockpit"] == nil || v["kernels"] == nil {
		t.Errorf("version --json: %v", v)
	}
	var working map[string]any
	for _, e := range ok("say", "--json").([]any) {
		if m := e.(map[string]any); m["name"] == "working-on" {
			working = m
		}
	}
	if f, _ := working["fields"].(map[string]any)["by-session"].(map[string]any); f["filledBy"] != testrepo.SessionID {
		t.Errorf("say --json does not say by-session is filled from the identity: %v", working)
	}
	if qs := ok("ask", "--json").([]any); len(qs) == 0 || qs[0].(map[string]any)["name"] != "record" {
		t.Errorf("ask --json: %v", qs)
	}
	tiers := ok("trust", "list", "--json").([]any)
	if len(tiers) != 1 || tiers[0].(map[string]any)["name"] != "self" || len(tiers[0].(map[string]any)["keys"].([]any)) == 0 {
		t.Errorf("trust list --json: %v", tiers)
	}
	if srcs := ok("federation", "list", "--json").([]any); len(srcs) != 0 {
		t.Errorf("federation list --json with no sources: %v", srcs)
	}
	if c := ok("config", "--json").(map[string]any); c["sessionId"] != testrepo.SessionID || c["config"] == nil {
		t.Errorf("config --json: %v", c)
	}
	if st := ok("index", "status", "--json").(map[string]any); st["kind"] == nil {
		t.Errorf("index status --json: %v", st)
	}
	if runs := ok("run", "list", "--json").([]any); len(runs) != 0 {
		t.Errorf("run list --json with no runs: %v", runs)
	}
	doc, _ := parse("doctor", "--json")
	id := doc.(map[string]any)["fields"].(map[string]any)["identity"].(map[string]any)
	if id["name"] != testrepo.SessionID || id["tier"] != "self" || id["nektonKeyId"] == "" {
		t.Errorf("doctor --json does not say who signs here: %v", id)
	}

	for _, tc := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"say", "working-on", "work/out.csv", "--json"}, 2, "argument"},
		{[]string{"say", "no-such-template", "work/out.csv", "--json"}, 2, "template.not-allowed"},
		{[]string{"say", `{"template":"working-on"}`}, 2, "argument"},
		{[]string{"index", "nonsense", "--json"}, 1, "error"},
	} {
		v, code := parse(tc.args...)
		m, _ := v.(map[string]any)
		if code != tc.exit || m["code"] != tc.code || m["reason"] == "" {
			t.Errorf("cockpit %s: want exit %d with code %q, got exit %d: %v", strings.Join(tc.args, " "), tc.exit, tc.code, code, v)
		}
	}
}
