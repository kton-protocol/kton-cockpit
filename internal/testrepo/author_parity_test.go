package testrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
)

// The linked authoring path and `plankton author` must produce the SAME foton id.
//
// A foton's identity is computed over its descriptor, so every default in the assembly is
// load-bearing: the protocol kind, the shape of {cmd, environment, envRef}, whether a locator is
// carried, and what counts as a logical path. Reading the reference implementation and reproducing
// it is exactly the kind of agreement that holds until it quietly does not — and the failure would
// be silent, because both sides would keep producing perfectly valid fotons that no longer meet.
//
// So it is checked against the binary rather than against a fixture. When the CLI is eventually
// gone this test goes with it, and what replaces it is that there is only one implementation left.
func TestAuthor_MatchesTheReferenceCLI(t *testing.T) {
	r := New(t)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/analyse.py", "print('deterministic')\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")

	const (
		cmd    = "python data/analyse.py data/in.csv > data/out.csv"
		envRef = "oci://example.test/img@sha256:" +
			"0000000000000000000000000000000000000000000000000000000000000000"
	)
	inputs := []string{"data/in.csv", "data/analyse.py"}
	outputs := []string{"data/out.csv"}
	located := []string{"data/in.csv=https://example.test/in.csv"}

	// The CLI, into a registry of its own so the two ingests cannot influence each other.
	cliDir := filepath.Join(t.TempDir(), "plankton")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"author"}
	for _, p := range inputs {
		args = append(args, "--in", p)
	}
	for _, p := range outputs {
		args = append(args, "--out", p)
	}
	for _, l := range located {
		args = append(args, "--located", l)
	}
	args = append(args, "--env-ref", envRef, "--cmd", cmd,
		"--sign", "keys/"+SessionID+".key", "--add", "--registry", cliDir, "--print-id")
	cli := exec.Command(filepath.Join(r.Root, "bin", "plankton"), args...)
	cli.Dir = r.Root
	cli.Env = append(os.Environ(), "PLANKTON_DIR="+cliDir)
	out, err := cli.Output()
	if err != nil {
		t.Fatalf("plankton author: %v", err)
	}
	fromCLI := strings.TrimSpace(string(out))

	// The library, through this cockpit's own path.
	cfg := r.Config(t)
	fromLib, err := binaries.New(cfg).Author(context.Background(), binaries.AuthorInput{
		Inputs: inputs, Outputs: outputs, Cmd: cmd, Located: located,
		SignKey: cfg.PlanktonKey, EnvRef: envRef,
	})
	if err != nil {
		t.Fatalf("linked author: %v", err)
	}

	if fromLib != fromCLI {
		t.Fatalf("the two authoring paths disagree on identity:\n  cli %s\n  lib %s\n"+
			"A descriptor default differs — kind, the descriptor's shape, a carried locator, or what "+
			"counts as a logical path.", fromCLI, fromLib)
	}
}
