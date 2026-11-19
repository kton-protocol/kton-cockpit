package cockpit

import (
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// allowsTemplate is the claim-template ceiling (SPEC §8.1): a template is allowed when the
// configuration names it (claims.allowedTemplates) or when an installed package the configuration
// admits brings it (claims.allowedPackages). The operator fixes the set either way — by template,
// or by the package that carries a whole vocabulary.
func allowsTemplate(cfg *config.Config, name string) bool {
	ok, _ := templateAllowed(cfg, name)
	return ok
}

// templateAllowed is allowsTemplate with the reason the vocabulary could not be read, for a caller
// that should say it: an allowed package failing its check, or a module the configuration names that
// this binary does not link, is not the same as a template nobody allowed.
func templateAllowed(cfg *config.Config, name string) (bool, error) {
	return binaries.New(cfg).TemplateAllowed(name)
}
