//go:build docker

// These drive a real container runtime. They are behind a build tag so `go test ./...` keeps its
// property that nothing skips — a suite that quietly degrades to skips is how this one previously
// spent months verifying nothing. Asking for them and not having an engine is a failure, not a
// skip, because you asked:
//
//	go test -tags docker ./internal/tools/
//
// Run them before merging anything that touches internal/container or publish's execution path.

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A digest-pinned reference, which is the only kind the config accepts. Pinned here for the same
// reason the cockpit insists on it: a tag would make this test depend on whatever that tag points
// at today.
const testImage = "oci://alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"

func executingRepo(t *testing.T, network bool) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Execution = config.Execution{Image: testImage, Network: network}
	r.WriteConfig(t, raw)
	r.Use(t)
	return r
}

// The property the whole design turns on: the cockpit RAN the command, and the environment the
// foton pins is the image it ran it in — not a value the config asserted alongside a run that
// happened somewhere else.
func TestContainer_PublishRunsTheCommandAndPinsWhatItRanIn(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "id,value\n1,42\n2,\n")

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "grep -v ',$' data/in.csv > data/out.csv",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}

	// The output did not exist before the run — nothing in this test wrote it.
	produced, rerr := os.ReadFile(filepath.Join(r.Root, "data/out.csv"))
	if rerr != nil {
		t.Fatalf("the container produced no output file: %v", rerr)
	}
	if string(produced) != "id,value\n1,42\n" {
		t.Fatalf("the command did not run as written; got %q", produced)
	}
	if out.ExecutedIn != testImage {
		t.Errorf("executedIn: got %q, want %q", out.ExecutedIn, testImage)
	}
	if out.EnvRef != testImage {
		t.Errorf("the foton pinned %q, but the command ran in %q — these must be the same string", out.EnvRef, testImage)
	}
	if out.NetworkAllowed {
		t.Error("the run was isolated, but publish reported networkAllowed")
	}
}

// A failed run is a failed publish. A foton describing whatever a failed run left behind would
// assert work that never completed — and it would be signed.
func TestContainer_AFailedRunPublishesNothing(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")
	before := r.HeadSHA(t)

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "echo nope >&2; exit 3",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("publish succeeded despite the command failing in the container")
	}
	if !strings.Contains(errText(result), "nope") {
		t.Errorf("the container's stderr did not reach the error: %s", errText(result))
	}
	if r.HeadSHA(t) != before {
		t.Error("a failed run still produced a commit")
	}
}

// Declaring an output the run does not produce must not reach plankton as a confusing file-not-
// found: publish declares outputs that have to exist once the command has run.
func TestContainer_ARunThatProducesNoDeclaredOutputFails(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/never.csv"},
		Cmd:     "true",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("publish succeeded although the declared output was never produced")
	}
	if !strings.Contains(errText(result), "data/never.csv") {
		t.Errorf("the error does not name the missing output: %s", errText(result))
	}
}

// --network none is the default and it has to actually bite, not merely be passed on the command
// line. The run always produces the output either way and writes WHICH branch it took, so the
// assertion is on the observed result rather than on a failure that several causes could produce.
func TestContainer_TheDefaultRunHasNoNetwork(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "if getent hosts example.com >/dev/null 2>&1; then echo REACHED > data/out.csv; else echo ISOLATED > data/out.csv; fi",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}
	got, rerr := os.ReadFile(filepath.Join(r.Root, "data/out.csv"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.TrimSpace(string(got)) != "ISOLATED" {
		t.Fatalf("the container resolved a public hostname, so --network none did not take effect (wrote %q)", got)
	}
}

// The negative control for the test above. Without it, "ISOLATED" proves nothing: a container with
// no DNS configured at all would write the same thing, and the isolation test would pass while
// --network none did nothing. This is the only test here that deliberately opens the network.
func TestContainer_WithNetworkAllowedTheSameCommandReachesOut(t *testing.T) {
	r := executingRepo(t, true)
	r.Write(t, "data/in.csv", "x\n")

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "if getent hosts example.com >/dev/null 2>&1; then echo REACHED > data/out.csv; else echo ISOLATED > data/out.csv; fi",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}
	got, rerr := os.ReadFile(filepath.Join(r.Root, "data/out.csv"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.TrimSpace(string(got)) != "REACHED" {
		t.Fatalf("with network allowed the container still could not resolve a hostname (wrote %q) — "+
			"so the isolation test above is not discriminating between isolation and a container that "+
			"never had DNS in the first place", got)
	}
	if !out.NetworkAllowed {
		t.Error("the run was not isolated, but publish did not report networkAllowed")
	}
}

// Files the container writes must belong to the operator. A root-owned artefact is one git cannot
// stage and the operator cannot clean up.
func TestContainer_OutputsAreOwnedByTheInvokingUser(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "cp data/in.csv data/out.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("publish failed: err=%v %s", err, errText(result))
	}
	fi, serr := os.Stat(filepath.Join(r.Root, "data/out.csv"))
	if serr != nil {
		t.Fatal(serr)
	}
	if !ownedByCurrentUser(t, fi) {
		t.Error("the container wrote a file the invoking user does not own")
	}
}

// The declaration is held against what the run actually did. An output produced and not declared is
// otherwise invisible: the foton understates the work, nothing fails, and the omission surfaces much
// later as a chain that does not join.
func TestContainer_ReportsAFileTheRunWroteButThePublishDidNotDeclare(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		// Two files written, one declared.
		Cmd: "cp data/in.csv data/out.csv; cp data/in.csv data/forgotten.csv",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}

	var named bool
	for _, c := range out.UndeclaredChanges {
		if c == "data/forgotten.csv" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the undeclared file was not reported; got %v", out.UndeclaredChanges)
	}
	// Reported, not adopted: taking it as an output would put it into the foton's identity.
	if _, adopted := out.OutputHashes["data/forgotten.csv"]; adopted {
		t.Error("the undeclared file was recorded as an output")
	}
}

// Reporting is not failing. A run that leaves a temp file behind is doing something legitimate, and
// a publish that refused would be wrong far more often than right.
func TestContainer_AnUndeclaredChangeDoesNotFailThePublish(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "echo scratch > data/scratch.tmp; cp data/in.csv data/out.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("a temp file must not fail the publish: err=%v %s", err, errText(result))
	}
	if !strings.HasPrefix(out.FotonID, "sha256:") {
		t.Fatal("no foton was recorded")
	}
}

// A run whose declaration is complete says nothing, so the field is a signal rather than noise.
func TestContainer_NothingIsReportedWhenTheDeclarationIsComplete(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	_, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "cp data/in.csv data/out.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.UndeclaredChanges) != 0 {
		t.Fatalf("a complete declaration still reported changes: %v", out.UndeclaredChanges)
	}
}

// The finding this closes, run as the finding described it: a command that replaces bin/plankton
// would make the cockpit author the record with a kernel `doctor` never checked, and nothing would
// fail — what ran and what was recorded would simply diverge.
//
// `go build -o bin/plankton …` is a legitimate command straight out of this project's own
// instructions, which is what makes it worth closing rather than forbidding.
func TestContainer_TheTrustBaseIsNotInTheRoom(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "x\n")

	before := readFile(t, filepath.Join(r.Root, "bin", "plankton"))
	keyBefore := readFile(t, filepath.Join(r.Root, "keys", testrepo.SessionID+".key"))

	// One command, reporting what it can see and trying to overwrite each of them.
	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd: `{
		   echo "keys: $(ls keys 2>&1 | wc -l)"
		   echo "bin: $(ls bin 2>&1 | wc -l)"
		   echo "git: $(ls .git 2>&1 | wc -l)"
		   echo "registry: $(ls registry/plankton 2>&1 | wc -l)"
		   echo "config: $(wc -c < cockpit.config.json)"
		   echo replaced > bin/plankton 2>/dev/null || echo "bin/plankton: not writable through to the host"
		   echo stolen > keys/leak 2>/dev/null || true
		 } > data/out.csv 2>&1`,
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}
	t.Logf("what the container saw:\n%s", readFile(t, filepath.Join(r.Root, "data", "out.csv")))

	// The assertions that matter are on the HOST, after the run.
	if got := readFile(t, filepath.Join(r.Root, "bin", "plankton")); got != before {
		t.Fatal("the container replaced bin/plankton on the host — the cockpit would then author with it")
	}
	if got := readFile(t, filepath.Join(r.Root, "keys", testrepo.SessionID+".key")); got != keyBefore {
		t.Fatal("the container reached the signing key")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "keys", "leak")); err == nil {
		t.Fatal("the container wrote into the host's keys directory")
	}
	if !strings.HasPrefix(out.FotonID, "sha256:") {
		t.Fatal("the publish itself should still have succeeded")
	}

	// And the masking is real rather than the command merely having failed: the container saw the
	// directories as EMPTY, which is what "empty the room" means. `ls` on an empty dir prints
	// nothing, so wc -l is 0; the config is masked to /dev/null, so it reads as 0 bytes.
	saw := readFile(t, filepath.Join(r.Root, "data", "out.csv"))
	for _, want := range []string{"keys: 0", "bin: 0", "git: 0", "registry: 0", "config: 0"} {
		if !strings.Contains(saw, want) {
			t.Errorf("expected the container to see %q, got:\n%s", want, saw)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
