package spec_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The homepage is generated from the examples and from a captured transcript, so it can go stale in
// the one way a page about a tool must not: an example changes, the page keeps describing the old
// one, and the description is wrong while looking maintained.
//
// This is the same check the rest of this directory makes about the spec — a document that cites
// something must cite something that is there.
func TestDocs_TheHomepageIsWhatBuildPyProduces(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("python3", "docs/build.py", "--check")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docs/index.html is out of date with the repository it describes.\n"+
			"Re-run `python3 docs/build.py` (and `docs/capture.sh` first if a verb's answer "+
			"changed).\n%s", strings.TrimSpace(string(out)))
	}
}
