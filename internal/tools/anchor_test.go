package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/testrepo"
)

// These cover this side of the network: the record's envelope is found by id and handed to kton,
// the entry it prints is read, and the proof is attached to the record and committed with it. They
// do NOT cover that Rekor behaves as expected — the anchor call itself is stubbed, because it writes
// to a public, permanent log and a suite that did it on every run would leave entries nobody can
// withdraw. See testrepo.StubAnchor.

func anchoringRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Anchor = config.Anchor{Enabled: true}
	r.WriteConfig(t, raw)
	r.StubAnchor(t, 12345678, "24296fb24b8ad77a9f")
	r.Use(t)
	return r
}

func TestAnchor_TheProofIsAttachedToTheRecordAndCommittedWithIt(t *testing.T) {
	r := anchoringRepo(t)
	pub := publishOne(t, r)

	if pub.RekorLogIndex != 12345678 || pub.RekorUUID != "24296fb24b8ad77a9f" {
		t.Fatalf("the entry's coordinates were not reported: %+v", pub)
	}

	// Attached, and readable back through the substrate that stores it. The registry has to be
	// pinned the way the cockpit pins it — without it plankton reads its bare ./plankton-data
	// default, which is a different, empty registry that answers with no material and no error.
	cmd := exec.Command(r.Root+"/bin/plankton", "material", pub.FotonID, "--json")
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "PLANKTON_DIR="+filepath.Join(r.Root, "registry/plankton"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("plankton material: %v", err)
	}
	var read struct {
		Material []struct {
			Scheme   string `json:"scheme"`
			Material string `json:"material"`
		} `json:"material"`
	}
	if err := json.Unmarshal(out, &read); err != nil {
		t.Fatalf("could not read the attached material: %v\n%s", err, out)
	}
	if len(read.Material) != 1 {
		t.Fatalf("expected exactly the one attached proof, got %d: %s", len(read.Material), out)
	}
	m := read.Material[0]
	if m.Scheme != "rekor-entry" {
		t.Errorf("scheme: %q", m.Scheme)
	}
	// The kernel stores material without evaluating it (kton §8.1), so it must never report a
	// verdict about it. Asserted against the RAW json rather than the decoded struct: the field is
	// being removed upstream as a kton §8.1 violation — it makes a verification statement the clause
	// forbids the kernel from making — and once it is gone, a decoded bool would be checking a
	// zero value and passing for the wrong reason. Absent and false are both correct here; true
	// never is.
	if bytes.Contains(out, []byte(`"verified":true`)) || bytes.Contains(out, []byte(`"verified": true`)) {
		t.Error("the kernel reported the material as verified; it does not evaluate material")
	}
	blob, derr := base64.StdEncoding.DecodeString(m.Material)
	if derr != nil {
		t.Fatalf("the stored material is not readable: %v", derr)
	}
	if !strings.Contains(string(blob), pub.RekorUUID) {
		t.Fatalf("the stored proof does not contain the entry that was reported: %s", blob)
	}
}

func TestAnchor_ClaimsAreAnchoredToo(t *testing.T) {
	r := anchoringRepo(t)
	pub := publishOne(t, r)

	result, out, err := Say(context.Background(), nil, SayInput{
		Subject:  pub.FotonID,
		Template: "working-on",
		Fields:   map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil || result.IsError {
		t.Fatalf("say failed: err=%v %s", err, errText(result))
	}
	if out.RekorUUID == "" {
		t.Fatal("the claim was not anchored")
	}
}

// A witness that was not really recorded, reported as if it were, is worse than none: the record
// would carry a claim of independent corroboration that nothing backs.
func TestAnchor_AFailedAnchorFailsThePublish(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Anchor = config.Anchor{Enabled: true}
	// A custom endpoint that does not exist, with a pinned key so the config itself is valid.
	raw.Anchor.RekorURL = "http://127.0.0.1:1"
	raw.Anchor.RekorPubkey = "-----BEGIN PUBLIC KEY-----\nnot a key\n-----END PUBLIC KEY-----"
	r.WriteConfig(t, raw)
	r.Use(t)
	r.Write(t, "data/out.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"data/out.csv"}, Cmd: "true",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("publish reported success although the anchor could not be recorded")
	}
}

// A log that issues its own Signed Entry Timestamp and is then checked against its own key verifies
// nothing, so a fabricated entry would self-verify.
func TestAnchor_RefusesACustomLogWithNoPinnedKey(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Anchor = config.Anchor{Enabled: true, RekorURL: "https://rekor.example.invalid"}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a custom log with no pinned key was accepted")
	}
	if !strings.Contains(errText(result), "self-verify") {
		t.Fatalf("the error does not explain the problem: %s", errText(result))
	}
}
