package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// call writes one tool call's files into dir: what it read and what it wrote.
func call(t *testing.T, r *testrepo.Repo, dir string) {
	t.Helper()
	r.Write(t, dir+"/psn.mod", "$PROB run1\n$DATA pk.csv\n")
	r.Write(t, dir+"/pk.csv", "ID,TIME,DV\n1,0,0\n1,1,4.2\n")
	r.Write(t, dir+"/psn.lst", "OBJV 123.456\n")
}

func publishIn(t *testing.T, base string, inputs, outputs []string, cmd string) (PublishResult, error) {
	t.Helper()
	result, out, err := Publish(context.Background(), nil, PublishRequest{Base: base, Inputs: inputs, Outputs: outputs, Cmd: cmd})
	if err != nil {
		return out, err
	}
	if result.IsError {
		return out, &Refusal{Code: "refused", Reason: errText(result)}
	}
	return out, nil
}

func actionKey(t *testing.T, r *testrepo.Repo, id string) string {
	t.Helper()
	f, err := binaries.New(r.Config(t)).Foton(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	k, err := f.ActionKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// With a base, a record's paths and command are relative to it, so the same call in two
// directories is one computation: one action key, one record. Without a base, the directory is part
// of the paths, and the two are different computations (SPEC §7.8).
func TestPublish_TheSameCallUnderTwoBasesIsOneComputation(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	call(t, r, "models/vpc/NM_run1")
	call(t, r, "elsewhere/run1")

	in, out, cmd := []string{"psn.mod", "pk.csv"}, []string{"psn.lst"}, "nmfe75 psn.mod psn.lst"
	a, err := publishIn(t, "models/vpc/NM_run1", in, out, cmd)
	if err != nil {
		t.Fatal(err)
	}
	b, err := publishIn(t, "elsewhere/run1", in, out, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if a.FotonID != b.FotonID || actionKey(t, r, a.FotonID) != actionKey(t, r, b.FotonID) {
		t.Fatalf("one call under two bases gave two computations: %s, %s", a.FotonID, b.FotonID)
	}
	if a.Base != "models/vpc/NM_run1" || a.OutputHashes["psn.lst"] == "" {
		t.Fatalf("the answer speaks of paths relative to its base: %+v", a)
	}
	f, err := binaries.New(r.Config(t)).Foton(context.Background(), a.FotonID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Descriptor["cmd"] != cmd || f.Inputs[0].Path == "" || strings.Contains(f.Inputs[0].Path, "/") {
		t.Fatalf("the record is not relative to its base: cmd %v, inputs %+v", f.Protocol.Descriptor["cmd"], f.Inputs)
	}

	c, err := publishIn(t, "", []string{"models/vpc/NM_run1/psn.mod", "models/vpc/NM_run1/pk.csv"},
		[]string{"models/vpc/NM_run1/psn.lst"}, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if actionKey(t, r, c.FotonID) == actionKey(t, r, a.FotonID) {
		t.Fatalf("without a base the directory is part of the paths, and the key must differ")
	}
}

// A base must be a directory inside the repository, and nothing the call names may leave it: a
// record's paths never escape upward (kton §6.1).
func TestPublish_ABaseAndItsPathsStayInsideTheRepository(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	call(t, r, "models/run1")
	r.Write(t, "data/shared.csv", "x\n")
	for name, req := range map[string]PublishRequest{
		"a path above base":     {Base: "models/run1", Inputs: []string{"../../data/shared.csv"}, Outputs: []string{"psn.lst"}},
		"an output above base":  {Base: "models/run1", Inputs: []string{"psn.mod"}, Outputs: []string{"../out.lst"}},
		"a base above the repo": {Base: "../x", Inputs: []string{"psn.mod"}, Outputs: []string{"psn.lst"}},
		"an absolute base":      {Base: "/tmp", Inputs: []string{"psn.mod"}, Outputs: []string{"psn.lst"}},
		"a base not there":      {Base: "models/none", Inputs: []string{"psn.mod"}, Outputs: []string{"psn.lst"}},
	} {
		req.Cmd = "nmfe75 psn.mod psn.lst"
		result, _, err := Publish(context.Background(), nil, req)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || !strings.Contains(errText(result), "base") {
			t.Errorf("%s was not refused: %s", name, errText(result))
		}
	}
}

// basedRun stands in for an executor: it records the command it was handed and writes the output
// where a command run in its base would.
type basedRun struct{ cmd string }

func (b *basedRun) Run(_ context.Context, s executor.Step) (executor.Result, error) {
	b.cmd = s.Cmd
	return executor.Result{Image: s.Image}, os.WriteFile(filepath.Join(s.Root, "work", "out.csv"), []byte("2\n"), 0o644)
}

var fakeBased = &basedRun{}

func init() { executor.Register("test-based", fakeBased) }

// When the cockpit runs the command, it runs it in base; the record keeps the command as given.
func TestPublish_ACommandTheCockpitRunsRunsInItsBase(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Execution = config.Execution{Executor: "test-based", Image: testImageRef}
	r.WriteConfig(t, raw)
	r.Use(t)
	r.Write(t, "work/in.csv", "1\n")
	out, err := publishIn(t, "work", []string{"in.csv"}, []string{"out.csv"}, "double in.csv > out.csv")
	if err != nil {
		t.Fatal(err)
	}
	if fakeBased.cmd != "cd 'work' && double in.csv > out.csv" {
		t.Fatalf("the executor was handed %q", fakeBased.cmd)
	}
	f, err := binaries.New(r.Config(t)).Foton(context.Background(), out.FotonID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Descriptor["cmd"] != "double in.csv > out.csv" {
		t.Fatalf("the record holds %v", f.Protocol.Descriptor["cmd"])
	}
}
