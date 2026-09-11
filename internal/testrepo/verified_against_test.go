package testrepo

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// CLAUDE.md states which kernel commit this repo was verified against, and tells the reader to
// update it when it drifts. That made it a claim maintained by remembering — and it went three
// updates without moving, because each edit matched against a string that had already changed and
// silently did nothing.
//
// Go stamps vcs.revision into a binary, so the claim has a ground truth to be checked against. This
// turns the line from documentation into an assertion, which is the same move this repo makes
// everywhere else: a statement nothing checks is a statement that drifts.
var verifiedAgainstRe = regexp.MustCompile(`(?m)^\*\*Verified against:\*\* kton ` + "`dev`" + ` at ` + "`" + `([0-9a-f]{7,40})` + "`")

func TestVerifiedAgainst_NamesTheKernelTheBinariesWereBuiltFrom(t *testing.T) {
	root := cockpitRoot(t)
	doc, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := verifiedAgainstRe.FindSubmatch(doc)
	if m == nil {
		t.Fatal(`CLAUDE.md has no "**Verified against:** kton ` + "`dev`" + ` at ` + "`<commit>`" + `" line — ` +
			"which kernel the suite ran against is not something to leave unstated")
	}
	stated := string(m[1])

	// The binary is the ground truth. When it carries no revision — built from an archive, or
	// supplied through COCKPIT_TEST_PLANKTON — there is nothing to compare against, and that is an
	// absence of evidence rather than a mismatch.
	rev := buildRevision(filepath.Join(root, "bin", "plankton"))
	if rev == "" {
		// No revision to compare against — built from an archive, or supplied through
		// COCKPIT_TEST_PLANKTON. Absence of evidence, not a mismatch.
		return
	}
	if len(stated) > len(rev) || rev[:len(stated)] != stated {
		t.Fatalf("CLAUDE.md says the suite was verified against %s, but bin/plankton was built from "+
			"%.12s.\n\nThis is upstream having moved, not something wrong in this repo — the fixture "+
			"rebuilt to the newer kernel, so the suite really did run against a different one than the "+
			"line claims. Re-run the suite and update the line in the same commit; that pairing is the "+
			"whole point of the line.", stated, rev)
	}
}
