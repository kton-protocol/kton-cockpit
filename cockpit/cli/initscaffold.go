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
//go:embed skeleton/templates/*.json skeleton/gitignore
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

	if _, err := os.Stat(filepath.Join(root, ".gitignore")); os.IsNotExist(err) {
		b, _ := skeletonTemplates.ReadFile("skeleton/gitignore")
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), b, 0o644); err != nil {
			return nil, err
		}
		notes = append(notes, "wrote .gitignore (/keys/ stays private; registry/keys/ is committed)")
	} else if b, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil && !ignoresKeys(string(b), raw.Paths.KeysDir) {
		notes = append(notes, fmt.Sprintf("WARNING: .gitignore does not exclude /%s/ — add it, or a private key can be committed", raw.Paths.KeysDir))
	}

	ids := identities(filepath.Join(root, filepath.FromSlash(raw.Paths.KeysDir)))
	switch len(ids) {
	case 1:
		id := ids[0]
		keys := raw.Paths.KeysDir
		raw.Identity = config.Identity{SessionID: id, PlanktonKey: path.Join(keys, id+".key"),
			NektonKey: path.Join(keys, id+"-claims.key")}
		pubs, err := publishPublicKeys(root, raw, id)
		if err != nil {
			return nil, err
		}
		raw.Trust.Tiers["self"] = pubs
		notes = append(notes, fmt.Sprintf("signing as %s (%s/%s.key), trusted in tier self", id, keys, id))
		notes = append(notes, fmt.Sprintf("public keys in %s/ — commit them, a peer verifies with them", publicKeysDir(raw)))
	case 0:
		notes = append(notes, "no signing identity yet: run `cockpit keygen <name>` — it binds itself here and trusts itself in tier self")
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

// publicKeysDir is where a repository's public keys are published: keys/ beside the registries
// (registry/keys for the default layout). Unlike the root keys/, it is committed.
func publicKeysDir(raw *config.Raw) string {
	return path.Join(path.Dir(raw.Paths.PlanktonDir), "keys")
}

// publishPublicKeys copies both public halves of an identity into the published key directory and
// returns their repo-relative paths: a foton is verified against the first, a claim against the
// second. An existing file with other bytes is refused — it would be someone else's key under this
// name, and overwriting it would make this repository's own records fail to verify silently.
func publishPublicKeys(root string, raw *config.Raw, name string) ([]string, error) {
	dst := publicKeysDir(raw)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dst)), 0o755); err != nil {
		return nil, err
	}
	var out []string
	for _, f := range []string{name + ".pub", name + "-claims.pub"} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(raw.Paths.KeysDir), f))
		if err != nil {
			return nil, err
		}
		to := filepath.Join(root, filepath.FromSlash(dst), f)
		if have, err := os.ReadFile(to); err == nil && string(have) != string(b) {
			return nil, fmt.Errorf("%s/%s exists with a different key — not overwritten", dst, f)
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			return nil, err
		}
		out = append(out, path.Join(dst, f))
	}
	return out, nil
}

func ignoresKeys(gitignore, keysDir string) bool {
	for _, l := range strings.Split(gitignore, "\n") {
		switch strings.TrimSpace(l) {
		case "/" + keysDir + "/", "/" + keysDir, "/" + keysDir + "/*":
			return true
		}
	}
	return false
}
