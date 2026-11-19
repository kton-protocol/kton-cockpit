package show

import (
	"net/http"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// PackageKey is one admitted package's build key: the key that signed the package's records, which
// install --allow takes in. It is a label, not trust: the ring (keys.json) stays the configured tiers
// only (ADR-016 L2), and a viewer shows a record signed by such a key as "an admitted package's",
// a third state beside trusted and unknown (userinterface's graph@1).
type PackageKey struct {
	Package string `json:"package"` // the package's name
	ID      string `json:"id"`      // the package id
	Dir     string `json:"dir"`
}

// Packages maps the keyid of each admitted, readable package's seal signer to the package.
func Packages(cfg *config.Config) (map[string]PackageKey, error) {
	out := map[string]PackageKey{}
	admitted := map[string]bool{}
	for _, id := range cfg.Raw.Claims.AllowedPackages {
		admitted[id] = true
	}
	entries, err := packages.ListEach(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Err != nil || !admitted[e.Package.ID] || e.Seal.Signer == "" {
			continue
		}
		out[e.Seal.Signer] = PackageKey{Package: e.Package.Name, ID: e.Package.ID, Dir: e.Dir}
	}
	return out, nil
}

func (s *Server) servePackages(w http.ResponseWriter, _ *http.Request) {
	p, err := Packages(s.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, p)
}
