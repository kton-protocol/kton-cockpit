package cockpit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/scope"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

const nonmemImage = "oci://example.org/nonmem@sha256:4444444444444444444444444444444444444444444444444444444444444444"

// The environment class is named per call, as envRef is: a recorder publishes for several tools,
// and the class belongs to the tool that ran. Omitted, the repo's class is used (SPEC §7.5).
func TestPublish_TheCallerNamesTheEnvironmentClass(t *testing.T) {
	const class = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")
	req := PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "run data/in.csv", EnvRef: nonmemImage}

	res, plain, err := Publish(context.Background(), nil, req)
	if err != nil || res.IsError {
		t.Fatalf("publish without a class: %v %s", err, errText(res))
	}
	req.Environment = class
	res, named, err := Publish(context.Background(), nil, req)
	if err != nil || res.IsError {
		t.Fatalf("publish with a class: %v %s", err, errText(res))
	}
	if named.Environment != class || named.FotonID == plain.FotonID {
		t.Fatalf("the class did not reach the record: %+v vs %+v", named, plain)
	}
	f := fotonOf(t, named.FotonID)
	if got := descriptorEnvironment(f); got != class {
		t.Fatalf("descriptor.environment = %q, want %q", got, class)
	}

	req.Environment = "sha256:nothex"
	if res, _, _ := Publish(context.Background(), nil, req); !res.IsError || !strings.Contains(errText(res), "environment") {
		t.Fatalf("a class that names nothing must be refused: %s", errText(res))
	}
}

// A reproduction with no normaliser named is judged through the one that comes with the environment
// class of the run being reproduced: the spectrum's normaliser, found in an installed package. Bytes
// that differ only where the normaliser strips reach L1, and the answer says where its via came
// from (SPEC §8.2).
func TestSay_ReproducesThroughTheNormaliserOfTheRunsEnvironment(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Reproduction.RequiredLevel = "L1"
	r.WriteConfig(t, raw)
	r.Use(t)
	ctx := context.Background()

	// Two applications of one normaliser: the recorded run's .lst and the re-run's, both to the
	// same normalised bytes. The first application stands for the potential (plankton takes a
	// normaliser foton id as well as its ref).
	recorded, rerun := "run 1\ntime: 10:00\nOFV 123.4\n", "run 1\ntime: 11:30\nOFV 123.4\n"
	apply := func(raw string) PublishResult {
		r.Write(t, "norm/run.lst", raw)
		r.Write(t, "norm/run.norm", "run 1\nOFV 123.4\n")
		return mustPublish(t, PublishRequest{Inputs: []string{"norm/run.lst"}, Outputs: []string{"norm/run.norm"},
			Cmd: "sh normalize-nonmem.sh norm/run.lst norm/run.norm", EnvRef: nonmemImage})
	}
	first := apply(recorded)
	apply(rerun)

	spec := ktonpkg.Spectrum{Spectrum: "nonmem-7.5", Normalizer: first.FotonID,
		Members: []ktonpkg.SpectrumMember{{Name: "THEO-101", Output: "sha256:" + strings.Repeat("7", 64)}}}
	class, err := spec.ID()
	if err != nil {
		t.Fatal(err)
	}
	installSpectrum(t, spec)

	r.Write(t, "data/model.ctl", "$PROBLEM theo\n")
	r.Write(t, "data/run.lst", recorded)
	subject := mustPublish(t, PublishRequest{Inputs: []string{"data/model.ctl"}, Outputs: []string{"data/run.lst"},
		Cmd: "nmfe75 data/model.ctl data/run.lst", EnvRef: nonmemImage, Environment: class})
	r.Write(t, "data/rerun.lst", rerun)

	req := SayRequest{Subject: subject.FotonID, Template: "reproduces",
		SubjectOutputHash: subject.OutputHashes["data/run.lst"], ReproducedOutput: "data/rerun.lst", ReproducedFotonID: subject.FotonID}
	res, out, err := Say(ctx, nil, req)
	if err != nil || res.IsError {
		t.Fatalf("say reproduces through the spectrum's normaliser: %v %s", err, errText(res))
	}
	if out.Level != "L1" || out.Via != first.FotonID || !strings.HasPrefix(out.ViaFrom, "spectrum "+class) {
		t.Fatalf("want L1 via the spectrum's normaliser, got level %q via %q from %q", out.Level, out.Via, out.ViaFrom)
	}

	// Without the package, there is no normaliser for that class and the bytes differ.
	t.Run("without the spectrum", func(t *testing.T) {
		r2 := testrepo.New(t)
		r2.WriteConfig(t, raw)
		r2.Use(t)
		r2.Write(t, "data/model.ctl", "$PROBLEM theo\n")
		r2.Write(t, "data/run.lst", recorded)
		s := mustPublish(t, PublishRequest{Inputs: []string{"data/model.ctl"}, Outputs: []string{"data/run.lst"},
			Cmd: "nmfe75 data/model.ctl data/run.lst", EnvRef: nonmemImage, Environment: class})
		r2.Write(t, "data/rerun.lst", rerun)
		res, _, _ := Say(ctx, nil, SayRequest{Subject: s.FotonID, Template: "reproduces",
			SubjectOutputHash: s.OutputHashes["data/run.lst"], ReproducedOutput: "data/rerun.lst", ReproducedFotonID: s.FotonID})
		if !res.IsError || !strings.Contains(errText(res), "do not match") {
			t.Fatalf("with no spectrum installed, differing bytes must not reproduce: %s", errText(res))
		}
	})
}

func mustPublish(t *testing.T, in PublishRequest) PublishResult {
	t.Helper()
	res, out, err := Publish(context.Background(), nil, in)
	if err != nil || res.IsError {
		t.Fatalf("publish %s: %v %s", in.Cmd, err, errText(res))
	}
	return out
}

func fotonOf(t *testing.T, id string) *core.Foton {
	t.Helper()
	cfg, err := config.Load(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	f, err := binaries.New(cfg).Foton(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// installSpectrum builds a sealed tool package carrying spec and installs it into the current repo.
func installSpectrum(t *testing.T, spec ktonpkg.Spectrum) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	mustNil(t, err)
	dir := filepath.Join(t.TempDir(), "nonmem-spectrum")
	p, err := scope.Create(dir, "nonmem-spectrum", key, scope.Options{})
	mustNil(t, err)
	rev, err := p.NewRevision("v1")
	mustNil(t, err)
	mustNil(t, rev.SetProfile(scope.ProfileTool))
	raw, err := json.Marshal(spec)
	mustNil(t, err)
	h, err := p.PutBlob(raw)
	mustNil(t, err)
	sid, err := spec.ID()
	mustNil(t, err)
	_, err = rev.Claim(h, scope.PartOf, map[string]any{"value": map[string]any{"role": scope.RoleSpectrum, "spectrum": sid}})
	mustNil(t, err)
	_, err = rev.Seal()
	mustNil(t, err)
	if _, err := New(Start{}).Install(context.Background(), InstallRequest{Path: dir}); err != nil {
		t.Fatalf("install the spectrum package: %v", err)
	}
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
