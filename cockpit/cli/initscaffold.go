package cli

// initscaffold.go is what `init` sets up beyond the configuration, so that the three verbs work in
// the repository it was run in: the .kton/ tree (ADR-016), the claim templates the configuration
// allows, .kton/.gitignore, and the signing identity when the machine's key folder holds exactly one.
// It writes nothing that exists already.

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/dirfiles"
)

// The same files as uat/participant-skeleton/templates; TestInitTemplates_MatchTheSkeleton keeps
// them so.
//
//go:embed skeleton/templates/*.json skeleton/gitignore
var skeletonTemplates embed.FS

// scaffold makes the .kton/ tree, writes the allowed templates that are missing and .kton/.gitignore,
// and binds an identity when ~/.kton-keys holds exactly one. It returns what a person should know or
// do next.
func scaffold(root string, raw *config.Raw) ([]string, error) {
	var notes []string
	lay := config.Layout(root, *raw)
	for _, d := range []string{lay.PlanktonDir, lay.NektonDir, lay.TemplatesDir, filepath.Join(lay.KeysDir, "self"), lay.LocalDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	for _, name := range raw.Claims.AllowedTemplates {
		dst := filepath.Join(lay.TemplatesDir, name+".json")
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := skeletonTemplates.ReadFile("skeleton/templates/" + name + ".json")
		if err != nil {
			continue // not one this binary carries; the operator brings it
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return nil, err
		}
		notes = append(notes, fmt.Sprintf("wrote %s/%s.json", config.Rel("templates"), name))
	}

	ignore := filepath.Join(lay.KtonDir, ".gitignore")
	if _, err := os.Stat(ignore); os.IsNotExist(err) {
		b, _ := skeletonTemplates.ReadFile("skeleton/gitignore")
		if err := os.WriteFile(ignore, b, 0o644); err != nil {
			return nil, err
		}
		notes = append(notes, "wrote .kton/.gitignore (.kton/local/ is this checkout's own and never shared)")
	}

	if _, err := os.Stat(config.IdentityPath(root)); err == nil {
		return notes, nil // this checkout signs already
	}
	ring := keysDir()
	ids := identities(ring)
	switch len(ids) {
	case 1:
		pubs, err := bindIdentity(root, ids[0], ring)
		if err != nil {
			return nil, err
		}
		notes = append(notes, fmt.Sprintf("signing as %s (%s), trusted in tier self", ids[0], filepath.Join(ring, ids[0]+".key")))
		notes = append(notes, fmt.Sprintf("public keys %s — commit them, a peer verifies with them", strings.Join(pubs, ", ")))
	case 0:
		notes = append(notes, "no signing identity yet: run `cockpit keygen <name>` — it binds itself here and trusts itself in tier self")
	default:
		notes = append(notes, fmt.Sprintf("several identities in %s (%s): run `cockpit keygen <name>` with the one this checkout signs with",
			ring, strings.Join(ids, ", ")))
	}
	return notes, nil
}

// keysDir is where this machine's private keys live: $KTON_KEYS, or ~/.kton-keys (ADR-016 section 2).
func keysDir() string {
	if d := os.Getenv("KTON_KEYS"); d != "" {
		return d
	}
	return config.ExpandHome("~/.kton-keys")
}

// identities are the names with both halves present: <name>.key and <name>-claims.key.
func identities(dir string) []string {
	matches, _ := dirfiles.Named(dir, "", "-claims.key")
	var out []string
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), "-claims.key")
		if _, err := os.Stat(filepath.Join(dir, name+".key")); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// bindIdentity makes this checkout sign as name, with the keys in ring: it writes
// .kton/local/identity.json and copies both public halves into tier self (.kton/keys/self/), and
// returns their repo-relative paths. A public key already there with other bytes is refused — it
// would be someone else's key under this name.
func bindIdentity(root, name, ring string) ([]string, error) {
	pubs, err := publishPublicKeys(root, name, ring, "self")
	if err != nil {
		return nil, err
	}
	id := config.Identity{SessionID: name, PlanktonKey: filepath.Join(ring, name+".key"),
		NektonKey: filepath.Join(ring, name+"-claims.key")}
	return pubs, writeIdentity(root, id)
}

func writeIdentity(root string, id config.Identity) error {
	p := config.IdentityPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := jsonIndent(id)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// publishPublicKeys copies both public halves of an identity into a tier's folder and returns their
// repo-relative paths: a foton is verified against the first, a claim against the second.
func publishPublicKeys(root, name, ring, tier string) ([]string, error) {
	dst := filepath.Join(root, config.Dir, "keys", tier)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	var out []string
	for _, f := range []string{name + ".pub", name + "-claims.pub"} {
		b, err := os.ReadFile(filepath.Join(ring, f))
		if err != nil {
			return nil, err
		}
		to := filepath.Join(dst, f)
		rel := config.Rel("keys/" + tier + "/" + f)
		if have, err := os.ReadFile(to); err == nil && string(have) != string(b) {
			return nil, fmt.Errorf("%s exists with a different key — not overwritten", rel)
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, nil
}

func jsonIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
