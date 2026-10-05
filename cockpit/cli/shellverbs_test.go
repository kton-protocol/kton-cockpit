package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The three verbs typed as words: report a result, claim something about a file, ask about it —
// no JSON written, answers a person reads. The JSON form beside it is unchanged.
func TestShellVerbs_ReportClaimAskWithoutJSON(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "data/in.csv", "a\n1\n")
	r.Write(t, "work/out.csv", "a\n2\n")

	expect := func(code int, out string, want ...string) {
		t.Helper()
		if code != 0 {
			t.Fatalf("exit %d:\n%s", code, out)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("answer lacks %q:\n%s", w, out)
			}
		}
	}
	out, code := r.Cockpit(t, "publish", "--in", "data/in.csv", "--out", "work/out.csv", "--", "double", "data/in.csv")
	expect(code, out, "record   sha256:", "work/out.csv")

	out, code = r.Cockpit(t, "say", "working-on", "work/out.csv", "step=checking it", "by-session=s1")
	expect(code, out, "→ its bytes", "claim    sha256:")

	out, code = r.Cockpit(t, "ask", "about", "work/out.csv")
	expect(code, out, "working-on", `"step":"checking it"`)
	out, code = r.Cockpit(t, "ask", "producer", "work/out.csv")
	expect(code, out, "double data/in.csv")
	out, code = r.Cockpit(t, "ask", "by", "signer", "me")
	expect(code, out, "working-on")
	out, code = r.Cockpit(t, "ask", "by", "predicate", "working-on")
	expect(code, out, "working-on")

	// The JSON form is what it was: JSON in, JSON out.
	out, code = r.Cockpit(t, "ask", `{"query":"producer","ref":"work/out.csv"}`)
	var got map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Errorf("the JSON form no longer answers JSON (exit %d):\n%s", code, out)
	}
}

// A claim missing a field the template requires, or naming one it does not have, is refused with
// the names — before anything is signed.
func TestShellSay_NamesWhatIsMissingOrWrong(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "work/out.csv", "a\n2\n")
	out, code := r.Cockpit(t, "say", "working-on", "work/out.csv", "step=x")
	if code == 0 || !strings.Contains(out, "by-session") {
		t.Errorf("a missing field was not named (exit %d):\n%s", code, out)
	}
	out, code = r.Cockpit(t, "say", "working-on", "work/out.csv", "step=x", "bysession=s1")
	if code == 0 || !strings.Contains(out, "no field bysession") {
		t.Errorf("an unknown field was not named (exit %d):\n%s", code, out)
	}
	out, code = r.Cockpit(t, "ask", "by", "signer", "me")
	if code != 0 || strings.Contains(out, "working-on") {
		t.Errorf("a refused claim was written anyway:\n%s", out)
	}
}

func TestShellJoin(t *testing.T) {
	if got := shellJoin([]string{"python3", "a b.py", "it's"}); got != `python3 'a b.py' 'it'\''s'` {
		t.Errorf("got %s", got)
	}
}
