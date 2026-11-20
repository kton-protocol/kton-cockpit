package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A repository opens a scope of a name once: a second seed of the name is refused and names the scope
// that exists, so a retry after a failed push cannot make the name mean two scopes (qm-dev-provenance,
// 20.11.; SPEC §8.5).
func TestScopeSeed_ANameIsOpenedOnce(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out, code := r.Cockpit(t, "scope", "seed", "dryrun")
	if code != 0 {
		t.Fatalf("scope seed (exit %d):\n%s", code, out)
	}
	id := regexp.MustCompile(`sha256:[0-9a-f]{64}`).FindString(out)
	if id == "" {
		t.Fatalf("scope seed printed no id:\n%s", out)
	}
	out, code = r.Cockpit(t, "scope", "seed", "dryrun")
	if code == 0 || !strings.Contains(out, "already") || !strings.Contains(out, id) {
		t.Errorf("a second seed of the name: want refused naming %s, got exit %d:\n%s", id, code, out)
	}
	if out, code := r.Cockpit(t, "scope", "seed", "another"); code != 0 {
		t.Errorf("another name is still opened (exit %d):\n%s", code, out)
	}
}
