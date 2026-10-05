//go:build !unreleased

// Without the unreleased tag the cockpit builds against published kernels only, and the scope format
// (ktonpkg/scope) is not among them. Nothing installed can be read here, so no package contributes
// to the vocabulary; a configuration that names one is refused rather than silently ignored.
package packages

import "fmt"

// Dir is where installed packages live, relative to the repository root.
const Dir = "packages"

// Source says where a template or query came from.
type Source struct {
	Package string // package id
	Name    string
	Dir     string
}

// Vocabulary is what the allowed packages contribute: templates and queries by name.
type Vocabulary struct {
	Templates map[string][]byte
	Queries   map[string][]byte
	From      map[string]Source // "template:<name>" / "query:<name>" -> where it came from
}

// Allowed returns an empty vocabulary, or refuses when the configuration names packages.
func Allowed(repoRoot string, allowed []string) (*Vocabulary, error) {
	if len(allowed) > 0 {
		return nil, fmt.Errorf("claims.allowedPackages names %d package(s), but this build cannot read installed packages (built without the unreleased tag)", len(allowed))
	}
	return &Vocabulary{Templates: map[string][]byte{}, Queries: map[string][]byte{}, From: map[string]Source{}}, nil
}
