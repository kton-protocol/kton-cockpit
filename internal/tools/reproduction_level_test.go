package tools

import "testing"

// These test reproductionLevelRe against plankton's actual printed output shapes, not synthetic
// approximations. The L0 case below was captured live from the real vendored plankton binary
// running exactly Michael's reported scenario (`plankton reproduces $H $H --via some-normalizer`
// on byte-identical content) — confirming the fix: plankton reports L0 here despite --via being
// passed, and the parser must extract "L0", not infer "L1" from via's mere presence.

func TestReproductionLevelRe_IdenticalBytesReportsL0EvenWithViaPassed(t *testing.T) {
	// Captured verbatim from a real run of:
	//   plankton reproduces sha256:ac106884... sha256:ac106884... --via some-normalizer
	// (identical hashes, --via still supplied) - the exact bug scenario Michael reported: the old
	// code inferred "L1" purely from via != "", which this output proves wrong.
	out := "reproduction: L0 - identical output bytes (expected for L0). Independence is attested by the\n" +
		"separate producer fotons + your reproduces claim, not by this byte compare.\n"

	m := reproductionLevelRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("expected a match, got none for output:\n%s", out)
	}
	if m[1] != "L0" {
		t.Fatalf("expected L0 (identical bytes, regardless of --via), got %q", m[1])
	}
}

func TestReproductionLevelRe_NonIdenticalViaMatchReportsL1(t *testing.T) {
	// Matches the reference source's plain `fmt.Printf("reproduction: %s\n", level)` for the
	// non-identical, --via-matched case (reference/cmd/plankton/main.go, `reproduces` case).
	out := "reproduction: L1\n"

	m := reproductionLevelRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("expected a match, got none for output:\n%s", out)
	}
	if m[1] != "L1" {
		t.Fatalf("expected L1, got %q", m[1])
	}
}

func TestReproductionLevelRe_NoMatchOnUnparseableOutput(t *testing.T) {
	// determineReproductionLevel only ever reaches this parse step when Reproduces() reported
	// ok=true (exit 0) - the "none" case (no L0/L1 match) exits non-zero and is caught earlier by
	// the !ok branch, so it should never actually reach here. This just confirms the regex fails
	// closed (no match, not a wrong match) against output it was never meant to parse, rather than
	// e.g. loosely matching "none" as some level.
	out := "reproduction: none (no L0/L1 match - an L2 comparator verdict is required)\n"

	if m := reproductionLevelRe.FindStringSubmatch(out); m != nil {
		t.Fatalf("expected no match against a non-L0/L1/L2 output, got %v", m)
	}
}
