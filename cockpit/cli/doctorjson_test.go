package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// doctorOut runs doctor with args and returns what it printed and the error it ended with.
func doctorOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	rd, wr, err := os.Pipe()
	must(t, err)
	orig := os.Stdout
	os.Stdout = wr
	derr := runDoctor(context.Background(), args)
	os.Stdout = orig
	must(t, wr.Close())
	b, err := io.ReadAll(rd)
	must(t, err)
	return string(b), derr
}

// --json is the readiness a frontend's badge shows, as JSON only, and "not ready" is an error of
// its own (exit 3) rather than a zero exit a script has to parse text to see through.
func TestDoctorJSON_SaysReadyAndWhatIsMissing(t *testing.T) {
	r := testrepo.New(t)
	t.Chdir(r.Root)

	out, err := doctorOut(t, "--json")
	if err != nil {
		t.Fatalf("a ready repository: %v\n%s", err, out)
	}
	var rep doctorReport
	if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil {
		t.Fatalf("not JSON alone: %v\n%s", jerr, out)
	}
	if !rep.Ready || rep.Fields.NektonKey.State != stateOK || rep.Fields.RepoRoot == "" {
		t.Fatalf("want ready with its nekton key ok, got %+v", rep)
	}

	must(t, os.Remove(r.Config(t).NektonKey))
	out, err = doctorOut(t, "--json")
	if !errors.Is(err, errNotReady) {
		t.Fatalf("a repository without its claim key: want errNotReady, got %v", err)
	}
	rep = doctorReport{}
	must(t, json.Unmarshal([]byte(out), &rep))
	must := false
	for _, s := range rep.Steps {
		must = must || s.Must
	}
	if rep.Ready || !must || rep.Fields.NektonKey.State != stateMissing {
		t.Fatalf("want not ready, a step marked must and the key missing, got %+v", rep)
	}

	// The text says the same, and ends the same way.
	if _, err := doctorOut(t); !errors.Is(err, errNotReady) {
		t.Errorf("text doctor on a repository that is not ready: want errNotReady, got %v", err)
	}
	if _, err := doctorOut(t, "--yaml"); err == nil || errors.Is(err, errNotReady) {
		t.Errorf("an unknown argument: want an error, got %v", err)
	}
}
