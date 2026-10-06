package cli

// refs.go turns what a person has in hand into what a verb takes. At a shell nobody has a foton id;
// they have a file, or a run folder they just ran. So a reference may be any of:
//
//	sha256:…        as it is
//	runs/<slug>     the record that run folder's last `cockpit run` made
//	<file>          the hash of its bytes — a claim about a file is a claim about those bytes
//
// None of this is a lookup the verbs could not do; it is the step before them that a person would
// otherwise do by hand, with `sha256sum` and by reading JSON.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
)

// resolveRef returns what ref stands for, and says in words what it resolved to ("" when it was
// already an id).
func resolveRef(ctx context.Context, cfg *config.Config, ref string) (string, string, error) {
	if strings.HasPrefix(ref, "sha256:") {
		return ref, "", nil
	}
	abs := ref
	if !filepath.IsAbs(abs) {
		cwd, _ := os.Getwd()
		abs = filepath.Join(cwd, ref)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", "", fmt.Errorf("%s is not a file, a run folder, or a sha256: id", ref)
	}
	if fi.IsDir() {
		rel, rerr := filepath.Rel(filepath.Join(cfg.RepoRoot, runsDir), abs)
		if rerr != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
			return "", "", fmt.Errorf("%s is a directory but not a run folder (%s/<slug>)", ref, runsDir)
		}
		id, err := runFoton(ctx, cfg, runsDir+"/"+rel)
		if err != nil {
			return "", "", err
		}
		return id, fmt.Sprintf("%s/%s → its record %s", runsDir, rel, short16(id)), nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", "", err
	}
	h := core.HashBytes(b)
	return h, fmt.Sprintf("%s → its bytes %s", ref, short16(h)), nil
}

// runFoton is the record a run folder's `cockpit run` made: a verified producer of what is in its
// out/ whose command ran in that folder.
func runFoton(ctx context.Context, cfg *config.Config, rel string) (string, error) {
	outs, _ := filesUnderDir(cfg.RepoRoot, rel+"/out")
	if len(outs) == 0 {
		return "", fmt.Errorf("%s has not run yet (out/ is empty): cockpit run %s", rel, strings.TrimPrefix(rel, runsDir+"/"))
	}
	ans, err := cockpit.New(cockpit.Start{}).Ask(ctx, cockpit.AskRequest{Query: "producer", Ref: outs[0]})
	if err != nil {
		return "", err
	}
	r := binaries.New(cfg)
	for _, id := range ans.Included {
		if rec, err := r.FotonByID(ctx, id); err == nil && strings.HasPrefix(rec.Cmd, "cd "+rel+" && ") {
			return id, nil
		}
	}
	return "", fmt.Errorf("%s has outputs but no record of its own run made them — run it: cockpit run %s",
		rel, strings.TrimPrefix(rel, runsDir+"/"))
}

// ownClaimsKeyID is the keyid this repository signs claims with: `me` in `ask by signer me`.
func ownClaimsKeyID(cfg *config.Config) (string, error) {
	pub, err := publicOf(cfg.NektonKey)
	if err != nil {
		return "", err
	}
	p, err := core.ParsePublicKeyHex(pub)
	if err != nil {
		return "", err
	}
	return core.KeyIDHex(p), nil
}
