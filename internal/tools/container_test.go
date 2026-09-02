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

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/testrepo"
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
