package packages

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// aliasFile is the kernel's alias format (kton.dev/aliases/v0): CURIE prefixes, term aliases, and
// template short names.
type aliasFile struct {
	Prefixes  map[string]string `json:"prefixes,omitempty"`
	Terms     map[string]string `json:"terms,omitempty"`
	Templates map[string]string `json:"templates,omitempty"`
}

// AliasesWith merges the repository's alias file with the admitted packages' own (ktonpkg carries a
// package's aliases.json since v0.7.0), so a package's fields and queries resolve to the terms the
// package means (userinterface's S6 (2)). The repository's definitions win: the operator decides. Two
// packages defining one key alike merge; defining it differently, the key is left out for both and
// named in clashes, rather than resolved by order.
func (v *Vocabulary) AliasesWith(repo []byte) (merged []byte, clashes []string, err error) {
	var out aliasFile
	if len(repo) > 0 {
		if err := json.Unmarshal(repo, &out); err != nil {
			return nil, nil, fmt.Errorf("the repository's alias file: %w", err)
		}
	}
	if len(v.Aliases) == 0 {
		return repo, nil, nil
	}
	fromRepo := map[string]bool{}
	for kind, m := range map[string]map[string]string{"prefix": out.Prefixes, "term": out.Terms, "template": out.Templates} {
		for k := range m {
			fromRepo[kind+":"+k] = true
		}
	}
	type def struct{ value, by string }
	defs := map[string][]def{}
	for _, a := range v.Aliases {
		var f aliasFile
		if err := json.Unmarshal(a.Body, &f); err != nil {
			clashes = append(clashes, fmt.Sprintf("the package %s's alias file is not read: %v", a.Name, err))
			continue
		}
		for kind, m := range map[string]map[string]string{"prefix": f.Prefixes, "term": f.Terms, "template": f.Templates} {
			for k, val := range m {
				defs[kind+":"+k] = append(defs[kind+":"+k], def{val, a.Name})
			}
		}
	}
	set := func(kind, k, val string) {
		var m *map[string]string
		switch kind {
		case "prefix":
			m = &out.Prefixes
		case "term":
			m = &out.Terms
		default:
			m = &out.Templates
		}
		if *m == nil {
			*m = map[string]string{}
		}
		(*m)[k] = val
	}
	keys := make([]string, 0, len(defs))
	for k := range defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if fromRepo[key] {
			continue
		}
		ds := defs[key]
		same := true
		for _, d := range ds[1:] {
			same = same && d.value == ds[0].value
		}
		kind, k, _ := strings.Cut(key, ":")
		if same {
			set(kind, k, ds[0].value)
			continue
		}
		var who []string
		for _, d := range ds {
			who = append(who, d.by+" ("+d.value+")")
		}
		clashes = append(clashes, fmt.Sprintf("the %s %q is defined differently by %s: left out", kind, k, strings.Join(who, " and ")))
	}
	merged, err = json.Marshal(out)
	return merged, clashes, err
}
