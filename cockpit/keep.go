package cockpit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// A say keeps the bytes of the files it signs about, so the signed version can be opened after the
// working copy changed (userinterface's B11, MH 23.11., option a): a file git tracks goes into the content
// store .kton/blobs, committed and pushed with the claim; one git ignores goes into .kton/local/blobs, this
// checkout only, since ignored data was kept out of the shared history on purpose; one above
// claims.keepMaxBytes is not kept; claims.keep "off" keeps none. The answer says which.

// KeptFile is one file a say signed about, and where its bytes were kept.
type KeptFile struct {
	Path  string `json:"path"`
	Hash  string `json:"hash,omitempty"`
	Size  int64  `json:"size"`
	Where string `json:"where"`         // shared | local | no
	Why   string `json:"why,omitempty"` // for local and no
}

// keepSigned keeps each file of paths (repo-relative); want is the hash a file was signed as, "" when the
// caller has none to compare with. It reports whether anything went into the shared store, which the
// caller then persists with the claim.
func keepSigned(ctx context.Context, cfg *config.Config, paths []string, want map[string]string) ([]KeptFile, bool) {
	var out []KeptFile
	shared := false
	for _, rel := range paths {
		k := keepOne(ctx, cfg, rel, want[rel])
		shared = shared || k.Where == "shared"
		out = append(out, k)
	}
	return out, shared
}

func keepOne(ctx context.Context, cfg *config.Config, rel, want string) KeptFile {
	k := KeptFile{Path: rel, Where: "no"}
	if cfg.Raw.Claims.Keep == "off" {
		k.Why = "claims.keep is off"
		return k
	}
	if filepath.IsAbs(rel) || !filepath.IsLocal(rel) || strings.HasSuffix(rel, ".key") {
		k.Why = "not a file of the repository that may be kept"
		return k
	}
	abs := filepath.Join(cfg.RepoRoot, rel)
	st, err := os.Lstat(abs)
	if err != nil || !st.Mode().IsRegular() {
		k.Why = "not a regular file (a link is never kept)"
		return k
	}
	k.Size = st.Size()
	if max := cfg.Raw.Claims.KeepMaxBytesOrDefault(); st.Size() > max {
		k.Why = "larger than claims.keepMaxBytes"
		return k
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		k.Why = "it cannot be read"
		return k
	}
	k.Hash, k.Size = core.HashBytes(b), int64(len(b))
	if want != "" && !strings.EqualFold(want, k.Hash) {
		k.Hash, k.Why = "", "it changed after it was signed"
		return k
	}
	dir := cfg.BlobsDir
	k.Where = "shared"
	if ignoredByGit(ctx, cfg, rel) {
		dir, k.Where, k.Why = filepath.Join(cfg.LocalDir, "blobs"), "local", "git ignores it, so it stays in this checkout"
	}
	store, err := blobstore.Open(dir)
	if err == nil {
		_, err = store.Put(b)
	}
	if err != nil {
		k.Where, k.Why = "no", "the store cannot be written: "+err.Error()
	}
	return k
}

// ignoredByGit says whether git ignores rel in this repository. Without git (local mode) nothing is
// ignored; when git cannot tell, the file counts as ignored, so its bytes stay in this checkout.
func ignoredByGit(ctx context.Context, cfg *config.Config, rel string) bool {
	if cfg.Raw.Repo.Mode == config.ModeLocal {
		return false
	}
	err := exec.CommandContext(ctx, "git", "-C", cfg.RepoRoot, "check-ignore", "-q", "--", rel).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false // not ignored
	}
	return true // ignored (exit 0), or git could not say
}

// fileFieldPaths are the paths a say's file fields name, by its template, in field order.
func fileFieldPaths(set ntemplate.Set, template string, sets map[string]string) []string {
	t, ok := set.Get(template)
	if !ok {
		return nil
	}
	var names []string
	for name, f := range t.Fields {
		if f.Type == "file" && sets[name] != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		out = append(out, sets[n])
	}
	return out
}
