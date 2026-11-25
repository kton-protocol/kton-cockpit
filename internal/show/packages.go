package show

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg/scope"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/dirfiles"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// PackageKey is a build key that signed admitted packages: the key that signed their records, which
// install --allow takes in. It is a label, not trust: the ring (keys.json) stays the configured tiers
// only (ADR-016 L2), and a viewer shows a record signed by such a key as "an admitted package's", a
// third state beside trusted and unknown (userinterface's graph@1). One author's key may sign several
// packages, so it lists them all (kton-review K2 on #167).
type PackageKey struct {
	// Key is the public key in hex, so a viewer can re-check what it signed: the key the packages'
	// seals verify under.
	Key      string        `json:"key"`
	Packages []PackageInfo `json:"packages"`
}

// PackageInfo is one admitted package a build key signed.
type PackageInfo struct {
	Package string `json:"package"` // the package's name
	ID      string `json:"id"`      // the package id
	Dir     string `json:"dir"`
}

// Packages maps each key an admitted package's seal verifies under to the package, and names the
// collisions it left out. The signer is the key the seal's signature verifies under, among the
// public keys in the package's keys/, never the keyid the envelope declares (kton-review K1 on #167).
// A package that fails its check is left out, and so is a key that is in the trusted ring: the ring
// wins, and the collision is named.
func Packages(cfg *config.Config) (labels map[string]PackageKey, collisions []string, err error) {
	labels = map[string]PackageKey{}
	admitted := map[string]bool{}
	for _, id := range cfg.Raw.Claims.AllowedPackages {
		admitted[id] = true
	}
	ring := map[string]bool{}
	for p := range cfg.TierPubkeys() {
		if b, err := os.ReadFile(p); err == nil {
			if pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b))); err == nil {
				ring[core.KeyIDHex(pub)] = true
			}
		}
	}
	entries, err := packages.ListEach(cfg.RepoRoot)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if e.Err != nil || !admitted[e.Package.ID] || len(e.Package.Verify()) > 0 {
			continue
		}
		dir := filepath.Join(cfg.RepoRoot, filepath.FromSlash(e.Dir))
		env, ok := sealEnvelope(filepath.Join(dir, scope.NektonDir), e.Seal.ID)
		if !ok {
			continue
		}
		var pubs []ed25519.PublicKey
		files, _ := dirfiles.Named(filepath.Join(dir, scope.KeyDir), "", ".pub")
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil {
				if pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b))); err == nil {
					pubs = append(pubs, pub)
				}
			}
		}
		for i, ok := range core.VerifiedKeys(env, pubs) {
			if !ok {
				continue
			}
			kid := core.KeyIDHex(pubs[i])
			if ring[kid] {
				collisions = append(collisions, fmt.Sprintf("the admitted package %s is signed by %s, a key of the trusted ring: not labelled as the package's", e.Package.Name, kid))
				continue
			}
			l := labels[kid]
			l.Key = hex.EncodeToString(pubs[i])
			l.Packages = append(l.Packages, PackageInfo{Package: e.Package.Name, ID: e.Package.ID, Dir: e.Dir})
			labels[kid] = l
		}
	}
	sort.Strings(collisions)
	return labels, collisions, nil
}

// sealEnvelope is the signed envelope of the claim id in the package's nekton store.
func sealEnvelope(nektonDir, id string) (core.Envelope, bool) {
	reg, err := binaries.OpenNektonSource(nektonDir)
	if err != nil {
		return core.Envelope{}, false
	}
	for _, r := range reg.Records(0) {
		if r.ClaimID == id {
			return r.Envelope, true
		}
	}
	return core.Envelope{}, false
}

func (s *Server) servePackages(w http.ResponseWriter, _ *http.Request) {
	p, _, err := Packages(s.cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, p)
}
