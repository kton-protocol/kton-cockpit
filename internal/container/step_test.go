package container

import (
	"slices"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The step an executor gets hides the cockpit's own trust base: .kton/ (configuration, registries,
// public keys, the checkout's local state) and .git. Private keys and binaries are outside the
// repository altogether (ADR-016). An executor outside this repository learns that only from the step.
func TestStep_HidesTheTrustBase(t *testing.T) {
	cfg := config.Layout("/r", config.Raw{
		Execution: config.Execution{Image: "oci://x@sha256:00", Network: true, Engine: "podman"}})
	s := Step(cfg, "make")
	for _, d := range []string{".kton", ".git"} {
		if !slices.Contains(s.Hide, d) {
			t.Errorf("%s is not hidden: %v", d, s.Hide)
		}
	}
	if s.Root != "/r" || s.Image != "oci://x@sha256:00" || !s.Network || s.Engine != "podman" || s.Cmd != "make" {
		t.Errorf("step does not carry the configuration: %+v", s)
	}
}
