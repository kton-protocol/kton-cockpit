package spec_test

import (
	"os/exec"
	"strings"
	"testing"
)

// Every script this repository tells someone to run is executable in the INDEX.
//
// Checked here rather than left to CI, because CI catches it as exit 126 in the middle of a run
// and the message ("Process completed with exit code 126") names neither the file nor the cause.
// It has happened twice: once for all nine examples at the same time, and once for a single new
// one added after the first fix — a `chmod +x` that never reached git, because the working tree
// is on a filesystem that does not hold the bit.
//
// The mode git records is the only one that travels. What a fresh clone gets is this, not what the
// author's filesystem happened to report.
func TestExecutables_EveryScriptAnyoneIsToldToRunIsExecutable(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "-s", "--", "*.sh").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	// Sourced, never executed: marking it executable would say otherwise.
	sourced := map[string]bool{"examples/lib/common.sh": true}

	var wrong []string
	var seen int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mode, path := fields[0], fields[len(fields)-1]
		if sourced[path] {
			continue
		}
		seen++
		if mode != "100755" {
			wrong = append(wrong, path+" is "+mode)
		}
	}
	if seen == 0 {
		t.Fatal("no shell scripts found at all; this check verified nothing")
	}
	if len(wrong) > 0 {
		t.Fatalf("%d script(s) are not executable in git, so a fresh clone cannot run them.\n"+
			"Fix with `git update-index --chmod=+x <path>`:\n  %s",
			len(wrong), strings.Join(wrong, "\n  "))
	}
}
