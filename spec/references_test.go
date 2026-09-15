package spec_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every § reference outside SPEC.md names WHICH specification it means.
//
// The two numbering schemes collide: SPEC §11 is "Carried evidence" where kton §11 is "Registry,
// resolution, and completeness", and SPEC §13 and kton §13 differ likewise. Inside SPEC.md an
// unqualified reference unambiguously means that document, because a reader is holding it.
// Everywhere else — a Go comment, a commit message, a request sent upstream — there is no ambient
// "this document", and the reader may well be holding the other one.
//
// Checked rather than remembered because it has already happened: a request from this repository
// cited clauses 8.1 and 8.2 meaning SPEC.md, while kton numbers "Attached verification material"
// 8.1 and has no 8.2 at all. The requests were acted on anyway, but a reader six months from now
// would resolve them to the wrong clause.
func TestReferences_EverySectionCitationSaysWhichSpec(t *testing.T) {
	root := repoRoot(t)
	// The check is on what immediately PRECEDES the §, so the window has to include the qualifier
	// itself — a capture group ending at the § can never contain it, which is how the first version
	// of this test passed everything while checking nothing.
	ref := regexp.MustCompile(`.{0,14}§\s?\d`)

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "bin", ".work", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".md") {
			return nil
		}
		// SPEC.md is the one place a bare §N is correct, and the convention itself is stated there.
		if rel, _ := filepath.Rel(root, path); rel == filepath.Join("spec", "SPEC.md") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range ref.FindAllString(line, -1) {
				window := m
				if strings.Contains(window, "kton ") || strings.Contains(window, "SPEC ") ||
					strings.Contains(window, "SPEC.md") || strings.Contains(window, "Clause") {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+itoa(i+1)+": "+strings.TrimSpace(m))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d section reference(s) do not say which specification they mean.\n"+
			"Write `SPEC §N` for this project's spec and `kton §N` for the protocol's:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
