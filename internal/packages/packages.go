// Package packages is what this repository has installed: packages in the scope format (ktonpkg/
// scope), each kept whole under packages/<name>@<revision>/ — registries, bytes, keys and the
// readable view — so an installed package stays checkable as the document it was received as.
//
// It reads; it decides nothing about trust beyond the one rule the operator sets in the
// configuration: templates and queries come only from packages named in claims.allowedPackages, and
// only from their latest sealed revision.
package packages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit/module"
)

// Dir is where installed packages live, relative to the repository root.
const Dir = ".kton/packages"

// Installed is one installed package.
type Installed struct {
	Dir      string // repo-relative, packages/<name>@<label>
	Package  *scope.Package
	Revision *scope.Revision // the latest sealed revision
	Seal     scope.ClaimInfo
	Profile  string
}

// Entry is one directory under .kton/packages that holds a package: the package, or why it could
// not be read (Err). A listing that must show a broken package rather than fail uses ListEach.
type Entry struct {
	Dir string // repo-relative
	Installed
	Err error
}

// ListEach returns every installed package directory, sorted, each with its package or its error. A
// directory that is not a package in the scope format — a source package of this repo's own, an
// old-format one — is skipped, as List skips it.
func ListEach(repoRoot string) ([]Entry, error) {
	entries, err := os.ReadDir(filepath.Join(repoRoot, Dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), "@") {
			continue
		}
		abs := filepath.Join(repoRoot, Dir, e.Name())
		if _, err := os.Stat(filepath.Join(abs, scope.NektonDir)); err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(Dir, e.Name()))
		entry := Entry{Dir: rel}
		p, err := scope.Open(abs)
		if err != nil {
			entry.Err = err
			out = append(out, entry)
			continue
		}
		r, seal, err := p.Sealed()
		if err != nil {
			entry.Err = err
			out = append(out, entry)
			continue
		}
		prof, _ := r.Profile()
		entry.Installed = Installed{Dir: rel, Package: p, Revision: r, Seal: seal, Profile: prof}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// List returns the installed packages, sorted by directory, and fails on the first one that cannot
// be read.
func List(repoRoot string) ([]Installed, error) {
	all, err := ListEach(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, e := range all {
		if e.Err != nil {
			return nil, fmt.Errorf("%s: %w", e.Dir, e.Err)
		}
		out = append(out, e.Installed)
	}
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

// Vocabulary is what the allowed packages contribute: templates and queries by name, and the MCP
// tools a verticle declares (ktonpkg v0.3.0, profile verticle).
type Vocabulary struct {
	Templates map[string][]byte
	Queries   map[string][]byte
	MCPTools  map[string]scope.MCPTool
	From      map[string]Source // "template:<name>" / "query:<name>" / "mcp:<name>" -> where it came from
	// Refused are the admitted packages that contribute nothing: failing their check, unreadable, or
	// missing while some package directory cannot be read. The others keep working.
	Refused []Refused
	// Clashes are the names two sources bring, each offered only as <package>/<name>.
	Clashes []string
}

// contains reports whether in's package chain holds the revision id.
func contains(in Installed, rev string) bool {
	revs, err := in.Package.Revisions()
	if err != nil {
		return false
	}
	for _, r := range revs {
		if r.ID == rev {
			return true
		}
	}
	return false
}

// declaredNameProblem says why a package's templates cannot be offered: one declaring a name other
// than the one it is filed under, or a name with '/'.
func declaredNameProblem(ts map[string][]byte) string {
	names := make([]string, 0, len(ts))
	for n := range ts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, file := range names {
		var t struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(ts[file], &t)
		switch {
		case strings.Contains(file, "/") || strings.Contains(t.Name, "/"):
			return fmt.Sprintf("its template %q declares a name with '/', which is the cockpit's qualified form", file)
		case t.Name != "" && t.Name != file:
			return fmt.Sprintf("its template %s declares the name %q: a template declares the name it is filed under", file, t.Name)
		}
	}
	return ""
}

// Refused is an admitted package that contributes nothing, and why.
type Refused struct {
	Source
	Reason string
}

// Renamed gives a template's JSON the name it is offered under: the kernel's template set keys a
// template by the name it declares, so a qualified name must be the declared one. A body that is not
// a JSON object is returned as it is, and the kernel reports it.
func Renamed(body []byte, name string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	n, _ := json.Marshal(name)
	m["name"] = n
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// Allowed reads the templates and queries of the installed packages named in allowed. A package
// that fails its structural check contributes nothing and is reported: a vocabulary taken from a
// package whose signatures or bytes do not hold would be words nobody stands behind. Two allowed
// packages offering the same name is refused rather than resolved by order.
//
// modules are the linked modules the configuration admits (claims.allowedModules): their Words join
// the vocabulary as an allowed package's do, under the same rule that a name offered twice is
// refused. A module the configuration names but this binary does not link is an error, not a
// vocabulary quietly missing words.
func Allowed(repoRoot string, allowed, modules []string) (*Vocabulary, error) {
	v := &Vocabulary{Templates: map[string][]byte{}, Queries: map[string][]byte{}, MCPTools: map[string]scope.MCPTool{}, From: map[string]Source{}}
	if err := v.addModules(modules); err != nil {
		return nil, err
	}
	if len(allowed) == 0 {
		return v, nil
	}
	ok := map[string]bool{}
	for _, id := range allowed {
		ok[id] = true
	}
	// Each package directory on its own: a broken directory nobody admitted takes nothing away
	// (kton-review U2 on #152). One that cannot be read while an admitted package is missing may be
	// that package, so the vocabulary is then refused and names it.
	entries, err := ListEach(repoRoot)
	if err != nil {
		return nil, err
	}
	// Each admitted package on its own (userinterface's PR 2 (3), S6 (3)): one that fails its check or
	// cannot be read is listed under Refused and contributes nothing, and the others keep working. A
	// name two of them bring is offered under each one's qualified name, <package>/<name>, and under no
	// plain name, rather than refusing the whole vocabulary.
	type offer struct {
		src  Source
		body []byte
	}
	offers := map[string]map[string][]offer{"template": {}, "query": {}}
	tools := map[string][]struct {
		src  Source
		tool scope.MCPTool
	}{}
	var broken []string
	seen := map[string]bool{}
	// The admitted, readable installations, then: one per package id, the latest revision (an
	// installation of the same package whose revision the other's chain contains is older; two whose
	// chains do not meet are both refused; kton-review V3 on #162); and one per package name, since
	// the name qualifies a clashing word: two admitted packages of one name are both refused, so a
	// qualified name can never mean another key's package (kton-review V1 on #162).
	var admitted []Installed
	for _, e := range entries {
		if e.Err != nil {
			broken = append(broken, e.Dir+": "+e.Err.Error())
			continue
		}
		if ok[e.Package.ID] {
			seen[e.Package.ID] = true
			admitted = append(admitted, e.Installed)
		}
	}
	source := func(in Installed) Source { return Source{Package: in.Package.ID, Name: in.Package.Name, Dir: in.Dir} }
	byID := map[string][]Installed{}
	for _, in := range admitted {
		byID[in.Package.ID] = append(byID[in.Package.ID], in)
	}
	var latest []Installed
	for _, ins := range byID {
		best, ambiguous := ins[0], false
		for _, in := range ins[1:] {
			switch {
			case contains(in, best.Revision.ID):
				best = in
			case contains(best, in.Revision.ID):
			default:
				ambiguous = true
			}
		}
		if ambiguous {
			for _, in := range ins {
				v.Refused = append(v.Refused, Refused{Source: source(in), Reason: "installed more than once, as revisions neither of which follows the other"})
			}
			continue
		}
		latest = append(latest, best)
	}
	byName := map[string][]Installed{}
	for _, in := range latest {
		byName[in.Package.Name] = append(byName[in.Package.Name], in)
	}
	var usable []Installed
	for name, ins := range byName {
		if len(ins) == 1 {
			usable = append(usable, ins[0])
			continue
		}
		var dirs []string
		for _, in := range ins {
			dirs = append(dirs, in.Dir)
		}
		sort.Strings(dirs)
		for _, in := range ins {
			v.Refused = append(v.Refused, Refused{Source: source(in), Reason: fmt.Sprintf("two admitted packages are named %s (%s): a name must say which package it is", name, strings.Join(dirs, ", "))})
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].Dir < usable[j].Dir })
	for _, in := range usable {
		src := source(in)
		refuse := func(reason string) { v.Refused = append(v.Refused, Refused{Source: src, Reason: reason}) }
		if probs := in.Package.Verify(); len(probs) > 0 {
			refuse("fails its check: " + probs[0])
			continue
		}
		ts, err := in.Revision.Templates()
		if err != nil {
			refuse(err.Error())
			continue
		}
		// A template declares the name it is filed under, and no '/': the qualified form is the
		// cockpit's to give, and a declared one would make the kernel's set refuse every say
		// (kton-review V2 on #162).
		if why := declaredNameProblem(ts); why != "" {
			refuse(why)
			continue
		}
		qs, err := in.Revision.Queries()
		if err != nil {
			refuse(err.Error())
			continue
		}
		ms, err := in.Revision.MCPTools()
		if err != nil {
			refuse(err.Error())
			continue
		}
		for name, t := range ms {
			tools[name] = append(tools[name], struct {
				src  Source
				tool scope.MCPTool
			}{src, t})
		}
		for name, body := range ts {
			offers["template"][name] = append(offers["template"][name], offer{src, body})
		}
		for name, body := range qs {
			offers["query"][name] = append(offers["query"][name], offer{src, body})
		}
	}
	for kind, byName := range offers {
		dst := v.Templates
		if kind == "query" {
			dst = v.Queries
		}
		for name, offs := range byName {
			_, module := v.From[kind+":"+name]
			if len(offs) == 1 && !module {
				dst[name] = offs[0].body
				v.From[kind+":"+name] = offs[0].src
				continue
			}
			var who []string
			for _, o := range offs {
				q := o.src.Name + "/" + name
				body := o.body
				if kind == "template" {
					body = Renamed(body, q)
				}
				dst[q] = body
				v.From[kind+":"+q] = o.src
				who = append(who, o.src.Name)
			}
			if module {
				who = append(who, v.From[kind+":"+name].Name)
			}
			sort.Strings(who)
			v.Clashes = append(v.Clashes, fmt.Sprintf("the %s %q is brought by %s: use <package>/%s", kind, name, strings.Join(who, " and "), name))
		}
	}
	for name, ts := range tools {
		if len(ts) > 1 {
			var who []string
			for _, t := range ts {
				who = append(who, t.src.Name)
			}
			sort.Strings(who)
			v.Clashes = append(v.Clashes, fmt.Sprintf("the MCP tool %q is offered by %s: neither is served", name, strings.Join(who, " and ")))
			continue
		}
		v.MCPTools[name] = ts[0].tool
		v.From["mcp:"+name] = ts[0].src
	}
	sort.Strings(v.Clashes)
	if len(broken) > 0 {
		for _, id := range allowed {
			if !seen[id] {
				v.Refused = append(v.Refused, Refused{Source: Source{Package: id}, Reason: "not among the readable installed packages; some cannot be read: " + strings.Join(broken, "; ")})
			}
		}
	}
	return v, nil
}

// addModules adds the Words of the admitted modules.
func (v *Vocabulary) addModules(names []string) error {
	for _, name := range names {
		m, ok := module.Lookup(name)
		if !ok {
			return fmt.Errorf("claims.allowedModules names the module %q, which this cockpit binary does not link", name)
		}
		src := Source{Package: "module:" + name, Name: "module " + name, Dir: "built in"}
		ts, err := m.Templates()
		if err != nil {
			return err
		}
		qs, err := m.Queries()
		if err != nil {
			return err
		}
		for kind, words := range map[string]map[string][]byte{"template": ts, "query": qs} {
			dst := v.Templates
			if kind == "query" {
				dst = v.Queries
			}
			for w, body := range words {
				key := kind + ":" + w
				if prev, dup := v.From[key]; dup {
					return fmt.Errorf("two allowed vocabularies offer the %s %q: %s and %s", kind, w, prev.Name, src.Name)
				}
				dst[w] = body
				v.From[key] = src
			}
		}
	}
	return nil
}
