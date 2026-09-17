// Package spec_test holds the check that keeps SPEC.md true.
//
// The specification names, for every normative clause, the test that exercises it. That is the
// reason it is expected not to drift — but only if the names are real. A clause citing a test that
// was renamed or deleted still reads as evidence, and reads that way most convincingly to whoever
// is deciding whether to rely on the clause.
package spec_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// A citation is a Go test name. The trailing `*` of a "family" citation (TestFoo_*) is matched
// separately: it names a prefix that must have at least one member, not an identifier.
var (
	citationRe = regexp.MustCompile(`\bTest[A-Za-z0-9_]+\b`)
	familyRe   = regexp.MustCompile(`\bTest[A-Za-z0-9_]+_\*`)
	declRe     = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	// A leading clause number, e.g. "5.4 " or "12 ".
	clauseNumberRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?\s+`)
)

func TestSpec_EveryCitedTestExists(t *testing.T) {
	root := repoRoot(t)

	spec, err := os.ReadFile(filepath.Join(root, "spec", "SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	have := declaredTests(t, root)

	// Family citations first, so their prefixes are not then looked up as whole names.
	text := string(spec)
	for _, fam := range familyRe.FindAllString(text, -1) {
		prefix := strings.TrimSuffix(fam, "*")
		var found bool
		for name := range have {
			if strings.HasPrefix(name, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SPEC.md cites the family %s, and no test has that prefix", fam)
		}
		text = strings.ReplaceAll(text, fam, "")
	}

	cited := map[string]bool{}
	for _, name := range citationRe.FindAllString(text, -1) {
		cited[name] = true
	}
	if len(cited) == 0 {
		t.Fatal("SPEC.md cites no tests at all — either the citations went away or this check stopped finding them")
	}
	for name := range cited {
		if !have[name] {
			t.Errorf("SPEC.md cites %s, which no test declares — a clause citing a test that is not "+
				"there still reads as evidence", name)
		}
	}
	t.Logf("%d cited test names, all present", len(cited))
}

// Every clause that states a MUST should say how it is checked. A normative statement with no
// evidence behind it is the kind of specification that describes an intention.
func TestSpec_EveryNormativeSectionSaysHowItIsChecked(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join(repoRoot(t), "spec", "SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Sections are split on headings; a section containing "MUST" should contain "Checked by:" —
	// unless it is front matter, or places no requirement on an implementation. The exemptions are
	// listed one by one rather than pattern-matched, so adding one is a decision somebody makes
	// rather than a section quietly falling through.
	sections := regexp.MustCompile(`(?m)^#{2,3} `).Split(string(spec), -1)
	var missing []string
	for _, sec := range sections {
		// Matched on the title's NAME, with any leading clause number stripped. Renumbering a section
		// is an editorial act; it should not silently move a section out of the exemption list, which
		// is what matching on "14 Conformance" did the first time a clause was inserted above it.
		title := strings.TrimSpace(strings.SplitN(sec, "\n", 2)[0])
		title = clauseNumberRe.ReplaceAllString(title, "")
		if !strings.Contains(sec, "MUST") {
			continue
		}
		switch {
		case strings.Contains(sec, "Checked by:"),
			strings.HasPrefix(title, "Scope"), strings.HasPrefix(title, "Conventions"),
			strings.HasPrefix(title, "Terms"), strings.HasPrefix(title, "Normative references"),
			strings.HasPrefix(title, "Foreword"), strings.HasPrefix(title, "Introduction"),
			strings.HasPrefix(title, "Scope"), strings.HasPrefix(title, "Out of scope"),
			strings.HasPrefix(title, "Conventions"),
			strings.HasPrefix(title, "What a cockpit does not do"),
			strings.HasPrefix(title, "Conformance"),
			strings.HasPrefix(title, "What neither mode catches"), strings.HasPrefix(title, "The surface"),
			strings.HasPrefix(title, "Reading records"), strings.HasPrefix(title, "Anchoring"):
			continue
		default:
			missing = append(missing, title)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these sections state a MUST without saying how it is checked: %s",
			strings.Join(missing, "; "))
	}
}

func declaredTests(t *testing.T, root string) map[string]bool {
	t.Helper()
	out, err := exec.Command("grep", "-rhoE", `^func Test[A-Za-z0-9_]+\(`, root).Output()
	if err != nil {
		t.Fatalf("listing declared tests: %v", err)
	}
	have := map[string]bool{}
	for _, m := range declRe.FindAllStringSubmatch(string(out), -1) {
		have[m[1]] = true
	}
	if len(have) == 0 {
		t.Fatal("no test declarations found at all, so this check would pass vacuously")
	}
	return have
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate this source file")
	}
	return filepath.Dir(filepath.Dir(file))
}
