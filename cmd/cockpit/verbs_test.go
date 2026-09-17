package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// The three verbs are the whole point of the binary and, until they were added as subcommands,
// could not be typed: they existed only over MCP, so the examples drove them through an MCP client
// written in Python and every example demonstrated its own harness. These tests are about the
// command line specifically — that it reaches the same handlers, under the same guards, and says
// what happened in a way a shell can act on.

func publishOne(t *testing.T, r *testrepo.Repo) (out string, code int) {
	t.Helper()
	r.Write(t, "work/out.csv", "a,b\n1,2\n")
	return r.Cockpit(t, "publish",
		`{"cmd":"produce work/out.csv","inputs":[],"outputs":["work/out.csv"]}`)
}

func TestVerbs_PublishFromAShellRecordsARealFoton(t *testing.T) {
	r := testrepo.New(t)
	out, code := publishOne(t, r)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got struct {
		FotonID      string            `json:"fotonId"`
		OutputHashes map[string]string `json:"outputHashes"`
		CommitSHA    string            `json:"commitSha"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("what publish printed is not JSON a caller can read: %v\n%s", err, out)
	}
	if !strings.HasPrefix(got.FotonID, "sha256:") || len(got.FotonID) != 71 {
		t.Fatalf("no foton id in the answer: %q", got.FotonID)
	}
	if got.OutputHashes["work/out.csv"] == "" || got.CommitSHA == "" {
		t.Fatalf("the answer is missing what publish exists to report:\n%s", out)
	}
}

// --field is why the examples can read like commands instead of like JSON pipelines.
func TestVerbs_FieldPrintsTheBareValue(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "work/out.csv", "a,b\n1,2\n")
	out, code := r.Cockpit(t, "publish",
		`{"cmd":"produce work/out.csv","inputs":[],"outputs":["work/out.csv"]}`, "--field", "fotonId")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	id := strings.TrimSpace(out)
	if !strings.HasPrefix(id, "sha256:") || strings.ContainsAny(id, "{}\"") {
		t.Fatalf("--field did not print a bare value: %q", id)
	}
}

// A field that is not in the answer is said, not printed as empty: a script that cannot tell a
// missing field from an empty one will read the first as the second, silently.
func TestVerbs_AnUnknownFieldNamesWhatIsActuallyThere(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "work/out.csv", "a,b\n1,2\n")
	out, code := r.Cockpit(t, "publish",
		`{"cmd":"produce work/out.csv","inputs":[],"outputs":["work/out.csv"]}`, "--field", "fotonID")
	if code == 0 {
		t.Fatalf("a misspelled field was accepted:\n%s", out)
	}
	if !strings.Contains(out, "fotonId") {
		t.Fatalf("the error does not say what the answer actually has:\n%s", out)
	}
}

// Over MCP the tool schema catches a misspelled argument. At a shell nothing would, and
// `{"output":[...]}` for `outputs` would publish a record naming no outputs at all — signed, valid
// and wrong, which is the failure this repository exists to prevent one level down.
func TestVerbs_AMisspelledArgumentIsRefusedRatherThanIgnored(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "work/out.csv", "a,b\n1,2\n")
	out, code := r.Cockpit(t, "publish",
		`{"cmd":"produce work/out.csv","inputs":[],"output":["work/out.csv"]}`)
	if code == 0 {
		t.Fatalf("a record was published from an argument nobody checked:\n%s", out)
	}
	if !strings.Contains(out, "output") {
		t.Fatalf("the error does not name the field that was wrong:\n%s", out)
	}
}

// The guards are the configuration's, not the transport's. The anti-wrong-folder guard is the one
// this project exists for, so it is the one checked here: a session meets it, and so does a shell.
func TestVerbs_TheGuardsApplyAtTheCommandLineToo(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Repo.Owner = "someone-else"
	r.WriteConfig(t, raw)
	r.Write(t, "work/out.csv", "a,b\n1,2\n")

	out, code := r.Cockpit(t, "publish",
		`{"cmd":"produce work/out.csv","inputs":[],"outputs":["work/out.csv"]}`)
	if code == 0 {
		t.Fatalf("a config naming another repository was accepted:\n%s", out)
	}
	// Exit 2, not 1: the cockpit understood the call and declined it. A script can tell "you may
	// not" from "you asked wrong", which is the whole reason the two codes differ.
	if code != 2 {
		t.Fatalf("a refusal left by exit %d, not 2:\n%s", code, out)
	}
	if !strings.Contains(out, "someone-else") {
		t.Fatalf("the refusal does not say what it disagreed with:\n%s", out)
	}
}

// ask and say are reachable the same way. Checked together because what is being held here is the
// dispatch, not each verb's behaviour — those have their own tests in internal/tools.
func TestVerbs_AskAndSayAreReachableFromAShell(t *testing.T) {
	r := testrepo.New(t)
	out, code := publishOne(t, r)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var pub struct {
		FotonID      string            `json:"fotonId"`
		OutputHashes map[string]string `json:"outputHashes"`
	}
	if err := json.Unmarshal([]byte(out), &pub); err != nil {
		t.Fatal(err)
	}

	sayOut, sayCode := r.Cockpit(t, "say",
		`{"subject":"`+pub.FotonID+`","template":"working-on","fields":{"step":"from a shell","by-session":"session-1"}}`,
		"--field", "claimId")
	if sayCode != 0 {
		t.Fatalf("say exited %d:\n%s", sayCode, sayOut)
	}
	if !strings.HasPrefix(strings.TrimSpace(sayOut), "sha256:") {
		t.Fatalf("say printed no claim id: %q", sayOut)
	}

	askOut, askCode := r.Cockpit(t, "ask",
		`{"query":"producer","ref":"`+pub.OutputHashes["work/out.csv"]+`"}`)
	if askCode != 0 {
		t.Fatalf("ask exited %d:\n%s", askCode, askOut)
	}
	if !strings.Contains(askOut, pub.FotonID) {
		t.Fatalf("ask did not find the record that was just published:\n%s", askOut)
	}
}
