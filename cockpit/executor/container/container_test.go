package container

import (
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
)

func TestMaskArgs_EmptiesWhatTheStepHides(t *testing.T) {
	got := strings.Join(maskArgs(executor.Step{Hide: []string{"keys", ".git"}, HideFiles: []string{"cockpit.config.json"}}), " ")
	for _, want := range []string{"--tmpfs /work/keys", "--tmpfs /work/.git", "--volume /dev/null:/work/cockpit.config.json:ro"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestRegisteredAsTheDefault(t *testing.T) {
	if e, ok := executor.Lookup(""); !ok || e != (Executor{}) {
		t.Fatalf("the container executor is not the default: %v %v", e, ok)
	}
}
