package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	nclaim "kton.dev/nekton/claim"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// A say keeps the bytes of the files it signs about, so the signed version can be opened after the
// working copy changed (userinterface's B11, MH 23.11., option a): a file git tracks goes into the content
// store .kton/blobs, committed and pushed with the claim (with any uncommitted change, as it was signed);
// any other (untracked, ignored, or git cannot tell) goes into .kton/local/blobs, this checkout only;
// a private key is never kept; one above
// claims.keepMaxBytes is not kept; claims.keep "off" keeps none. The answer says which.

// beforeKeep runs between the signature and the keeping, for a test.
var beforeKeep func()

// KeptFile is one file a say signed about, and where its bytes were kept.
type KeptFile struct {
	Path  string `json:"path"`
	Hash  string `json:"hash,omitempty"`
	Size  int64  `json:"size"`
	Where string `json:"where"`         // shared | local | no
	Why   string `json:"why,omitempty"` // for local and no
}

// keepSigned keeps each file of paths (repo-relative); want is the hash a file was signed as, "" when the
// caller has none to compare with; signed are the hashes the stored claim carries, and a file's bytes are
// kept only when they hash to one of them, so a file changed between the signature and the keeping is not
// (kton-review on #248). It reports whether anything went into the shared store, which the caller then
// persists with the claim.
func keepSigned(ctx context.Context, cfg *config.Config, paths []string, want map[string]string, signed map[string]bool) ([]KeptFile, bool) {
	var out []KeptFile
	shared := false
	for _, rel := range paths {
		k := keepOne(ctx, cfg, rel, want[rel], signed)
		shared = shared || k.Where == "shared"
		out = append(out, k)
	}
	return out, shared
}

func keepOne(ctx context.Context, cfg *config.Config, rel, want string, signed map[string]bool) KeptFile {
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
	if (want != "" && !strings.EqualFold(want, k.Hash)) || (signed != nil && !signed[strings.ToLower(k.Hash)]) {
		k.Hash, k.Why = "", "it changed after it was signed"
		return k
	}
	// A key is never kept, in either store, whatever its name (kton-review K1 on #248).
	if looksLikePrivateKey(b) {
		k.Hash, k.Why = "", "it looks like a private key"
		return k
	}
	// Only what git already tracks goes into the shared, pushed store: the claim publishes a hash, and
	// bytes nobody chose to share (an untracked .env) must not follow it into history (K1).
	dir := cfg.BlobsDir
	k.Where = "shared"
	if !trackedByGit(ctx, cfg, rel) {
		dir, k.Where, k.Why = filepath.Join(cfg.LocalDir, "blobs"), "local", "git does not track it (untracked or ignored), so it stays in this checkout"
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

// trackedByGit says whether git tracks rel in this repository's index. Without git (local mode) every
// file counts as tracked: there is no shared history to push to. When git cannot tell, it does not.
func trackedByGit(ctx context.Context, cfg *config.Config, rel string) bool {
	if cfg.Raw.Repo.Mode == config.ModeLocal {
		return true
	}
	// Literal: ls-files reads a path as a glob otherwise, and an untracked data/a[1].txt would match a
	// tracked data/a1.txt (kton-review K2 on #248).
	return exec.CommandContext(ctx, "git", "--literal-pathspecs", "-C", cfg.RepoRoot, "ls-files", "--error-unmatch", "--", rel).Run() == nil
}

// looksLikePrivateKey: a PEM or OpenSSH private key, or a bare 32-byte seed in hex as keygen writes it.
func looksLikePrivateKey(b []byte) bool {
	if bytes.Contains(b, []byte("PRIVATE KEY-----")) {
		return true
	}
	return bareSeed.Match(bytes.TrimSpace(b))
}

var bareSeed = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// signedHashes are the sha256 values the stored claim id carries anywhere in its statement: its subjects,
// file fields and evidence.
func signedHashes(cfg *config.Config, id string) map[string]bool {
	out := map[string]bool{}
	reg, err := binaries.OpenNektonSource(cfg.NektonDir)
	if err != nil {
		return out
	}
	for _, r := range reg.RawRecords() {
		if !strings.EqualFold(r.ClaimID, id) {
			continue
		}
		st, _, err := nclaim.ParseEnvelope(r.Envelope)
		if err != nil {
			return out
		}
		js, _ := json.Marshal(st)
		for _, h := range anyHash.FindAllString(strings.ToLower(string(js)), -1) {
			out[h] = true
		}
		for _, sub := range st.Subject {
			if d := sub.Digest["sha256"]; d != "" {
				out["sha256:"+strings.ToLower(d)] = true
			}
		}
	}
	return out
}

var anyHash = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

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
