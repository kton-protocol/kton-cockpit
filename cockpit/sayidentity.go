package cockpit

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// IdentityField names who is speaking. A claim is signed, so the signature already says it; asking
// for it again only invites a value that disagrees with the signature. When a template declares it
// and the request leaves it out, say fills it with the identity it signs as (SPEC §8.7): the
// session of .kton/local/identity.json, or the key name a request acts as. Given explicitly, it is
// kept. Every form of say gets this: the shell, the JSON argument and the MCP tool.
const IdentityField = "by-session"

// FillIdentity sets fields[IdentityField] when template declares it and it is empty, and reports
// whether it did.
func FillIdentity(cfg *config.Config, template, as string, fields map[string]string) bool {
	if fields == nil || fields[IdentityField] != "" || !templateDeclares(cfg, template, IdentityField) {
		return false
	}
	who := cfg.Identity.SessionID
	if as != "" {
		who = as
	}
	if who == "" {
		return false
	}
	fields[IdentityField] = who
	return true
}

// templateDeclares reports whether the allowed template name has a field called field: from the
// template directory first, then from an admitted package, as allowsTemplate admits them.
func templateDeclares(cfg *config.Config, name, field string) bool {
	var b []byte
	if cfg.AllowsTemplate(name) {
		b, _ = os.ReadFile(filepath.Join(cfg.TemplatesDir, name+".json"))
	}
	if b == nil {
		if voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages, cfg.Raw.Claims.AllowedModules); err == nil {
			b = voc.Templates[name]
		}
	}
	var t struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if b == nil || json.Unmarshal(b, &t) != nil {
		return false
	}
	_, ok := t.Fields[field]
	return ok
}
