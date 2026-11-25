package packages

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Aliases are one admitted package's own CURIE prefixes and terms (its alias file, kton.dev/aliases/v0,
// which ktonpkg carries since v0.7.0). They resolve that package's templates and fields only: no
// package alias enters the set the repository's own templates resolve through, so one package cannot
// change what another's, or the repository's, words mean (kton-review A1 on #164).
type Aliases struct {
	Prefixes map[string]string `json:"prefixes,omitempty"`
	Terms    map[string]string `json:"terms,omitempty"`
}

// parseAliases reads a package's alias file. Template short names are refused: the kernel prefers a
// template alias over a real template name, which is how one package could redirect another's say.
func parseAliases(body []byte) (Aliases, error) {
	var f struct {
		Aliases
		Templates map[string]string `json:"templates"`
	}
	if err := json.Unmarshal(body, &f); err != nil {
		return Aliases{}, fmt.Errorf("its alias file is not read: %w", err)
	}
	if len(f.Templates) > 0 {
		return Aliases{}, fmt.Errorf("its alias file names template short names; a package may not (they would apply across packages)")
	}
	return f.Aliases, nil
}

// Resolve is the kernel's resolution (nekton template.Set.Resolve) over this package's aliases alone:
// a term first, then a CURIE prefix; anything else, and a full IRI, as it is.
func (a Aliases) Resolve(x string) string {
	if strings.Contains(x, "://") {
		return x
	}
	if v, ok := a.Terms[x]; ok {
		x = v
	}
	if i := strings.IndexByte(x, ':'); i >= 0 {
		if pfx, ok := a.Prefixes[x[:i]]; ok {
			return pfx + x[i+1:]
		}
	}
	return x
}

// Empty says the package defines no aliases.
func (a Aliases) Empty() bool { return len(a.Prefixes) == 0 && len(a.Terms) == 0 }

// ResolvedTemplate is a package template with its predicate, context and signerRoles resolved through
// the package's own aliases, so the shared template set receives full IRIs and needs no package alias
// (a role the set cannot resolve makes nekton refuse the template, kton-review F1 on #260). Every other
// member of the template is kept as it is.
func ResolvedTemplate(b []byte, a Aliases) []byte {
	if a.Empty() {
		return b
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	changed := false
	for _, k := range []string{"predicate", "context"} {
		var s string
		if raw, ok := m[k]; !ok || json.Unmarshal(raw, &s) != nil || s == "" {
			continue
		}
		if r := a.Resolve(s); r != s {
			m[k], _ = json.Marshal(r)
			changed = true
		}
	}
	var roles []string
	if raw, ok := m["signerRoles"]; ok && json.Unmarshal(raw, &roles) == nil {
		rolesChanged := false
		for i, r := range roles {
			if full := a.Resolve(r); full != r {
				roles[i], rolesChanged = full, true
			}
		}
		if rolesChanged {
			m["signerRoles"], _ = json.Marshal(roles)
			changed = true
		}
	}
	if !changed {
		return b
	}
	out, err := json.Marshal(m)
	if err != nil {
		return b
	}
	return out
}
