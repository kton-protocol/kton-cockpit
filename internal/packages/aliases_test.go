package packages

import (
	"testing"

	"kton.dev/nekton/template"
)

// Aliases.Resolve is the kernel's resolution over a package's aliases alone, so every value resolves
// as template.Set.Resolve does with the same prefixes and terms (kton #307: a scheme before "://").
func TestAliasesResolveMirrorsTheKernel(t *testing.T) {
	const file = `{"prefixes":{"ex":"https://example.test/v/","1a":"https://one.test/"},"terms":{"owns":"ex:owns"}}`
	set, err := template.New(nil, []byte(file))
	if err != nil {
		t.Fatal(err)
	}
	a, err := parseAliases([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []string{
		"ex:a://b", "ex:a", "owns", "https://x.test/y", "urn:qm:approves", "mailto:a@b.test",
		"://x", "1a://x", "1a:x", "zz:lead://x", "x y://z", "plain", "", "ex:", "HTTP://x", "did:ex:1",
	} {
		if got, want := a.Resolve(x), set.Resolve(x); got != want {
			t.Errorf("Resolve(%q) = %q, the kernel resolves %q", x, got, want)
		}
	}
	if got, want := a.Resolve("ex:a://b"), "https://example.test/v/a://b"; got != want {
		t.Errorf("ex:a://b resolves to %q, want %q (a CURIE of the prefix ex)", got, want)
	}
	for _, x := range []string{"://x", "1a://x"} {
		if isAbsoluteIRI(x) {
			t.Errorf("%q is not an absolute IRI", x)
		}
	}
}
