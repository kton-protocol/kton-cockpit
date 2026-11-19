package show

import (
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitmick/ktonpkg/scope"
	"kton.dev/plankton/core"

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
	// Key is the build key's public key in hex, from the package's keys/<keyid>.pub, so a viewer can
	// re-check what it signed. An entry is listed only when that key derives the keyid.
	Key string `json:"key"`
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
		b, err := os.ReadFile(filepath.Join(cfg.RepoRoot, filepath.FromSlash(e.Dir), scope.KeyDir, e.Seal.Signer+".pub"))
		if err != nil {
			continue
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
		if err != nil || core.KeyIDHex(pub) != e.Seal.Signer {
			continue
		}
		out[e.Seal.Signer] = PackageKey{Package: e.Package.Name, ID: e.Package.ID, Dir: e.Dir, Key: hex.EncodeToString(pub)}
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
