package cli

// initscaffold.go is what `init` sets up beyond the configuration, so that the three verbs work in
// the repository it was run in: the claim templates the configuration allows, and the signing
// identity, when there is exactly one to take. It writes nothing that exists already.

import (
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The same files as uat/participant-skeleton/templates; TestInitTemplates_MatchTheSkeleton keeps
// them so.
//
//go:embed skeleton/templates/*.json
var skeletonTemplates embed.FS

// scaffold writes the allowed templates that are missing and binds an identity found under the
// keys directory. It returns what a person should know or do next.
func scaffold(root string, raw *config.Raw) ([]string, error) {
	var notes []string

	tdir := filepath.Join(root, filepath.FromSlash(raw.Paths.TemplatesDir))
	for _, name := range raw.Claims.AllowedTemplates {
		dst := filepath.Join(tdir, name+".json")
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := skeletonTemplates.ReadFile("skeleton/templates/" + name + ".json")
		if err != nil {
			continue // not one this binary carries; the operator brings it
		}
		if err := os.MkdirAll(tdir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return nil, err
		}
		notes = append(notes, fmt.Sprintf("wrote %s/%s.json", raw.Paths.TemplatesDir, name))
	}

	ids := identities(filepath.Join(root, filepath.FromSlash(raw.Paths.KeysDir)))
	switch len(ids) {
	case 1:
		id := ids[0]
		keys := raw.Paths.KeysDir
		raw.Identity = config.Identity{SessionID: id, PlanktonKey: path.Join(keys, id+".key"),
			NektonKey: path.Join(keys, id+"-claims.key")}
		// Both halves: a foton is verified against the first, a claim against the second.
		raw.Trust.Tiers["self"] = []string{path.Join(keys, id+".pub"), path.Join(keys, id+"-claims.pub")}
		notes = append(notes, fmt.Sprintf("signing as %s (%s/%s.key), trusted in tier self", id, keys, id))
	case 0:
		notes = append(notes, "no signing identity yet: run `cockpit keygen <name>`, then set identity and trust.tiers as it says")
	default:
		notes = append(notes, fmt.Sprintf("several identities under %s/ (%s): set identity and trust.tiers to the one this repository signs with",
			raw.Paths.KeysDir, strings.Join(ids, ", ")))
	}
	return notes, nil
}

// identities are the names with both halves present: <name>.key and <name>-claims.key.
func identities(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*-claims.key"))
	var out []string
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), "-claims.key")
		if _, err := os.Stat(filepath.Join(dir, name+".key")); err == nil {
			out = append(out, name)
		}
	}
	return out
}
