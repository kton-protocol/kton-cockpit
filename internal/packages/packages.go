//go:build unreleased

// Package packages is what this repository has installed: packages in the scope format (ktonpkg/
// scope), each kept whole under packages/<name>@<revision>/ — registries, bytes, keys and the
// readable view — so an installed package stays checkable as the document it was received as.
//
// It reads; it decides nothing about trust beyond the one rule the operator sets in the
// configuration: templates and queries come only from packages named in claims.allowedPackages, and
// only from their latest sealed revision.
package packages

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg/scope"
)

// Dir is where installed packages live, relative to the repository root.
const Dir = "packages"

// Installed is one installed package.
type Installed struct {
	Dir      string // repo-relative, packages/<name>@<label>
	Package  *scope.Package
	Revision *scope.Revision // the latest sealed revision
	Seal     scope.ClaimInfo
	Profile  string
}

// List returns the installed packages, sorted by directory. A directory under packages/ that is not
// a package in the scope format — a source package of this repo's own, an old-format one — is not
// an installed package and is skipped.
func List(repoRoot string) ([]Installed, error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, Dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), "@") {
			continue
		}
		abs := filepath.Join(repoRoot, Dir, e.Name())
		if _, err := os.Stat(filepath.Join(abs, scope.NektonDir)); err != nil {
			continue
		}
		p, err := scope.Open(abs)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", Dir, e.Name(), err)
		}
		r, seal, err := p.Sealed()
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", Dir, e.Name(), err)
		}
		prof, _ := r.Profile()
		out = append(out, Installed{Dir: filepath.ToSlash(filepath.Join(Dir, e.Name())), Package: p, Revision: r,
			Seal: seal, Profile: prof})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// Find returns the installed package with this name; with several revisions installed, the one
// whose directory sorts last.
func Find(repoRoot, name string) (*Installed, error) {
	all, err := List(repoRoot)
	if err != nil {
		return nil, err
	}
	var hit *Installed
	var names []string
	for i := range all {
		names = append(names, all[i].Package.Name)
		if all[i].Package.Name == name {
			hit = &all[i]
		}
	}
	if hit == nil {
		if len(names) == 0 {
			return nil, fmt.Errorf("nothing is installed here (%s/ is empty)", Dir)
		}
		return nil, fmt.Errorf("no installed package %q; installed: %s", name, strings.Join(names, ", "))
	}
	return hit, nil
}

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

// Allowed reads the templates and queries of the installed packages named in allowed. A package
// that fails its structural check contributes nothing and is reported: a vocabulary taken from a
// package whose signatures or bytes do not hold would be words nobody stands behind. Two allowed
// packages offering the same name is refused rather than resolved by order.
func Allowed(repoRoot string, allowed []string) (*Vocabulary, error) {
	v := &Vocabulary{Templates: map[string][]byte{}, Queries: map[string][]byte{}, From: map[string]Source{}}
	if len(allowed) == 0 {
		return v, nil
	}
	ok := map[string]bool{}
	for _, id := range allowed {
		ok[id] = true
	}
	all, err := List(repoRoot)
	if err != nil {
		return nil, err
	}
	for _, in := range all {
		if !ok[in.Package.ID] {
			continue
		}
		if probs := in.Package.Verify(); len(probs) > 0 {
			return nil, fmt.Errorf("the allowed package %s (%s) fails its check: %s", in.Package.Name, in.Dir, probs[0])
		}
		src := Source{Package: in.Package.ID, Name: in.Package.Name, Dir: in.Dir}
		ts, err := in.Revision.Templates()
		if err != nil {
			return nil, err
		}
		qs, err := in.Revision.Queries()
		if err != nil {
			return nil, err
		}
		for kind, m := range map[string]map[string][]byte{"template": ts, "query": qs} {
			dst := v.Templates
			if kind == "query" {
				dst = v.Queries
			}
			for name, body := range m {
				key := kind + ":" + name
				if prev, dup := v.From[key]; dup {
					return nil, fmt.Errorf("two allowed packages offer the %s %q: %s and %s", kind, name, prev.Name, in.Package.Name)
				}
				dst[name] = body
				v.From[key] = src
			}
		}
	}
	return v, nil
}
