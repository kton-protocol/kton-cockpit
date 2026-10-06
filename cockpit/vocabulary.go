package cockpit

import (
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// allowsTemplate is the claim-template ceiling (SPEC §8.1): a template is allowed when the
// configuration names it (claims.allowedTemplates) or when an installed package the configuration
// admits brings it (claims.allowedPackages). The operator fixes the set either way — by template,
// or by the package that carries a whole vocabulary.
func allowsTemplate(cfg *config.Config, name string) bool {
	if cfg.AllowsTemplate(name) {
		return true
	}
	voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return false
	}
	_, ok := voc.Templates[name]
	return ok
}
