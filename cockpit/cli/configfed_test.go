package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// One value is set without opening the JSON; a value the configuration refuses is put back.
func TestConfig_SetsOneValueAndPutsBackWhatDoesNotLoad(t *testing.T) {
	r := testrepo.New(t)
	cfgPath := filepath.Join(r.Root, "cockpit.config.json")

	if out, code := r.Cockpit(t, "config", "git.push", "off"); code != 0 || !strings.Contains(out, "git.push = false") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if out, _ := r.Cockpit(t, "config"); !strings.Contains(out, "git.push") || !strings.Contains(out, "off") {
		t.Errorf("the overview does not show the change:\n%s", out)
	}
	before, _ := os.ReadFile(cfgPath)
	out, code := r.Cockpit(t, "config", "repo.name", "somewhere-else")
	if code == 0 || !strings.Contains(out, "unchanged") {
		t.Errorf("a binding that no longer matches the remote was kept (exit %d):\n%s", code, out)
	}
	if after, _ := os.ReadFile(cfgPath); string(after) != string(before) {
		t.Error("the refused change was left in the file")
	}
	if _, code := r.Cockpit(t, "config", "git.push", "--unset"); code != 0 {
		t.Error("unset failed")
	}
	if b, _ := os.ReadFile(cfgPath); strings.Contains(string(b), `"push"`) {
		t.Errorf("git.push still set:\n%s", b)
	}
}

// Another registry is read once added, its records counted against this repository's tiers; one
// that cannot be read is said and leaves the configuration as it was.
func TestFederation_AddListRemove(t *testing.T) {
	theirs := testrepo.New(t)
	theirs.Write(t, "work/out.csv", "a\n1\n")
	if out, code := theirs.Cockpit(t, "publish", "--out", "work/out.csv", "--", "make", "it"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	r := testrepo.New(t)
	out, code := r.Cockpit(t, "federation", "add", "peer", theirs.Root)
	if code != 0 || !strings.Contains(out, "1 record(s)") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if out, _ := r.Cockpit(t, "federation", "list"); !strings.Contains(out, "peer") {
		t.Errorf("list does not name the source:\n%s", out)
	}

	cfgPath := filepath.Join(r.Root, "cockpit.config.json")
	before, _ := os.ReadFile(cfgPath)
	if out, code := r.Cockpit(t, "federation", "add", "nothing", filepath.Join(t.TempDir(), "absent")); code == 0 {
		t.Errorf("an unreadable source was added:\n%s", out)
	}
	if after, _ := os.ReadFile(cfgPath); string(after) != string(before) {
		t.Error("the unreadable source was left in the configuration")
	}

	if out, code := r.Cockpit(t, "federation", "remove", "peer"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if b, _ := os.ReadFile(cfgPath); strings.Contains(string(b), `"peer"`) {
		t.Errorf("peer still configured:\n%s", b)
	}
}
