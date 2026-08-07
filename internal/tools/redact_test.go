package tools

import (
	"strings"
	"testing"
)

// hexID builds a syntactically valid fake sha256 id (recordIDRe requires exactly 64 [0-9a-f]
// characters) by repeating a single hex digit — distinct digits keep fixtures easy to tell apart.
func hexID(digit string) string {
	return "sha256:" + strings.Repeat(digit, 64)
}

func TestRedactExcluded_NoExclusionsLeavesRawUntouched(t *testing.T) {
	raw := hexID("a") + "  kind=script  in=1 out=1"
	got := redactExcluded(raw, nil)
	if got != raw {
		t.Fatalf("expected raw untouched with no exclusions, got %q", got)
	}
}

func TestRedactExcluded_ReplacesOnlyExcludedRecordLines(t *testing.T) {
	trusted := hexID("1")
	untrusted := hexID("2")
	raw := strings.Join([]string{
		trusted + "  kind=script  in=1 out=1",
		untrusted + "  kind=script  in=1 out=1",
	}, "\n")

	got := redactExcluded(raw, map[string]string{untrusted: "not verified against any configured trust tier"})
	lines := strings.Split(got, "\n")

	if lines[0] != strings.Split(raw, "\n")[0] {
		t.Fatalf("expected the trusted record's line to survive untouched, got %q", lines[0])
	}
	if strings.Contains(lines[1], "kind=script") {
		t.Fatalf("expected the excluded record's line to be redacted, still saw content: %q", lines[1])
	}
	if !strings.Contains(lines[1], untrusted) {
		t.Fatalf("expected the placeholder to still name which record was excluded, got %q", lines[1])
	}
}

func TestRedactExcluded_ReproductionsSummaryLineSurvivesEvenWhenRefWouldOtherwiseExclude(t *testing.T) {
	// Mirrors plankton's real reproductions output shape: a summary line that echoes the queried
	// ref hash, plus one line per producer foton. If the ref were left in the excluded set (it
	// normally resolves to Untrusted, since a query subject isn't itself a foton/claim id), this
	// summary line would get redacted too - destroying the actual answer. Ask() prevents that by
	// running ids through excludeRef before anything is classified as included/excluded; this test
	// exercises redactExcluded directly with what that produces.
	ref := hexID("3")
	producer := hexID("4")
	raw := strings.Join([]string{
		"reproductions: 1 distinct signer(s) produced " + ref + "  (↻1; 1 producer foton(s))",
		"  " + producer + "  by key:deadbeef",
	}, "\n")

	// excludedReasons deliberately does NOT contain ref - this is what Ask() guarantees via excludeRef.
	got := redactExcluded(raw, map[string]string{producer: "not verified against any configured trust tier"})
	lines := strings.Split(got, "\n")

	if !strings.Contains(lines[0], ref) || !strings.Contains(lines[0], "distinct signer(s)") {
		t.Fatalf("expected the summary line (containing the query ref) to survive untouched, got %q", lines[0])
	}
	if strings.Contains(lines[1], "by key:deadbeef") {
		t.Fatalf("expected the producer's line to be redacted, still saw content: %q", lines[1])
	}
}

func TestRedactExcluded_TrustedLineSurvivesEmbeddedUntrustedTermRef(t *testing.T) {
	// Regression test for the bug an independent cold-session review found: nekton's claim
	// predicate is spec-allowed to itself be a content hash (a "term reference", SPEC §2.2), so a
	// FULLY TRUSTED, verified claim's own printClaims line can legitimately contain a second,
	// unrelated sha256 hash - the predicate's term ref - which almost never resolves as a real
	// claim and so is almost always excluded in its own right. Naive "does this line contain any
	// excluded id anywhere" matching wiped the whole trusted claim's line because of that unrelated
	// embedded hash. redactExcluded must only redact based on the line's OWN (first) id.
	trustedClaim := hexID("5")
	untrustedTermRef := hexID("6")
	raw := trustedClaim + "  predicate=" + untrustedTermRef + "  by=agent:alice  declared-keyid=deadbeef (unverified)"

	got := redactExcluded(raw, map[string]string{untrustedTermRef: "not verified against any configured trust tier"})

	if got != raw {
		t.Fatalf("expected the trusted claim's line to survive untouched despite its embedded, unrelated "+
			"untrusted term-ref hash, got %q", got)
	}
}

func TestRedactExcluded_StillRedactsWhenTheLinesOwnLeadingIdIsExcluded(t *testing.T) {
	untrustedClaim := hexID("7")
	trustedTermRef := hexID("8")
	raw := untrustedClaim + "  predicate=" + trustedTermRef + "  by=agent:mallory  declared-keyid=badbeef (unverified)"

	got := redactExcluded(raw, map[string]string{untrustedClaim: "not verified against any configured trust tier"})

	if strings.Contains(got, "agent:mallory") {
		t.Fatalf("expected the untrusted claim's line (its OWN leading id is excluded) to be redacted, got %q", got)
	}
	if !strings.Contains(got, untrustedClaim) {
		t.Fatalf("expected the placeholder to still name which record was excluded, got %q", got)
	}
}

func TestExcludeRef_DropsTheQuerySubjectCaseInsensitively(t *testing.T) {
	ref := "sha256:ABCDEF"
	ids := []string{"sha256:abcdef", "sha256:other"}

	got := excludeRef(ids, ref)
	if len(got) != 1 || got[0] != "sha256:other" {
		t.Fatalf("expected only the non-ref id to remain, got %v", got)
	}
}

func TestExcludeRef_LeavesIdsUntouchedWhenRefAbsent(t *testing.T) {
	ids := []string{"sha256:a", "sha256:b"}
	got := excludeRef(ids, "sha256:c")
	if len(got) != 2 {
		t.Fatalf("expected both ids preserved, got %v", got)
	}
}
