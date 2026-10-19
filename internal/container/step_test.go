package container

import (
	"slices"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The step an executor gets hides the cockpit's own trust base: keys, binaries, both registries,
// .git and the configuration. An executor outside this repository learns that only from the step.
func TestStep_HidesTheTrustBase(t *testing.T) {
	cfg := &config.Config{RepoRoot: "/r", Raw: config.Raw{
		Paths: config.Paths{KeysDir: "keys", BinDir: "bin", PlanktonDir: "registry/plankton", NektonDir: "registry/nekton"},
		Execution: config.Execution{Image: "oci://x@sha256:00", Network: true, Engine: "podman"}}}
	s := Step(cfg, "make")
	for _, d := range []string{"keys", "bin", "registry/plankton", "registry/nekton", ".git"} {
		if !slices.Contains(s.Hide, d) {
			t.Errorf("%s is not hidden: %v", d, s.Hide)
		}
	}
	if !slices.Contains(s.HideFiles, "cockpit.config.json") {
		t.Errorf("the configuration is not hidden: %v", s.HideFiles)
	}
	if s.Root != "/r" || s.Image != "oci://x@sha256:00" || !s.Network || s.Engine != "podman" || s.Cmd != "make" {
		t.Errorf("step does not carry the configuration: %+v", s)
	}
}
