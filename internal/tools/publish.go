package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PublishInput is cockpit_publish's argument shape — the "veröffentlichen" verb. Claude supplies
// plain repo-relative paths and the command that was run; the cockpit owns every permalink,
// commit, and signature.
type PublishInput struct {
	Inputs  []string `json:"inputs" jsonschema:"repo-relative paths this computation consumed"`
	Outputs []string `json:"outputs" jsonschema:"repo-relative paths this computation produced"`
	Cmd     string   `json:"cmd" jsonschema:"the exact command that was run to produce the outputs"`
	// Corpus lists nekton claim or plankton foton refs (sha256:...) that this result's own
	// reasoning drew on as its basis. When set, the cockpit records a small corpus manifest file
	// as an extra foton input, so the reasoning-as-basis is itself a registered foton whose
	// inputs are the touched nektons — never a silent re-attestation ("Lake bleibt Lake").
	Corpus []string `json:"corpus,omitempty" jsonschema:"nekton claim or plankton foton refs (sha256:...) this reasoning drew on as its basis, if any"`
}

type PublishOutput struct {
	FotonID string `json:"fotonId"`
	// omitempty matters here, not just for tidiness: without it, jsonschema-go marks this field
	// required, and a nil map (the zero value returned on any error path) marshals to JSON null —
	// which then fails the SDK's own output-schema validation with a confusing "type: null, want
	// object" error that masks whatever the real error was.
	OutputHashes map[string]string `json:"outputHashes,omitempty"`
	CommitSHA    string            `json:"commitSha"`
	Permalinks   map[string]string `json:"permalinks,omitempty"`
}

func Publish(ctx context.Context, _ *mcp.CallToolRequest, in PublishInput) (*mcp.CallToolResult, PublishOutput, error) {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return errResult[PublishOutput]("%v", err)
	}
	if !cfg.Raw.Verbs.Publish {
		return errResult[PublishOutput]("publish is disabled by this repo's cockpit.config.json")
	}
	if len(in.Outputs) == 0 {
		return errResult[PublishOutput]("publish requires at least one output path")
	}
	if in.Cmd == "" {
		return errResult[PublishOutput]("publish requires cmd (the command that produced the outputs)")
	}

	r := binaries.New(cfg)
	allPaths := append(append([]string{}, in.Inputs...), in.Outputs...)

	// Claude only gets three narrow verbs; the cockpit owns key hygiene — but without this check,
	// publish is a general "commit and push any path in this repo" primitive, since git itself
	// doesn't care whether a staged path is a signing key. Validate every input/output path BEFORE
	// touching git or plankton at all, and report every denied path at once (not just the first),
	// so a Claude retry can fix everything in one pass.
	var denied []string
	for _, p := range allPaths {
		if verr := validatePublishPath(cfg, p); verr != nil {
			denied = append(denied, verr.Error())
		}
	}
	if len(denied) > 0 {
		return errResult[PublishOutput]("publish refused %d path(s):\n%s", len(denied), strings.Join(denied, "\n"))
	}

	var corpusPath string
	if len(in.Corpus) > 0 {
		corpusPath = filepath.Join("corpus", fmt.Sprintf("%s-%d.json", cfg.Raw.Identity.SessionID, time.Now().UnixNano()))
		abs := filepath.Join(cfg.RepoRoot, corpusPath)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return errResult[PublishOutput]("could not create corpus manifest dir: %v", err)
		}
		b, _ := json.MarshalIndent(map[string]any{"corpus": in.Corpus}, "", "  ")
		if err := os.WriteFile(abs, b, 0o644); err != nil {
			return errResult[PublishOutput]("could not write corpus manifest: %v", err)
		}
		allPaths = append(allPaths, corpusPath)
	}

	sha, err := gitops.CommitAndPush(ctx, cfg, allPaths, "publish: "+in.Cmd)
	if err != nil {
		return errResult[PublishOutput]("git commit/push of inputs+outputs failed: %v", err)
	}

	located := gitops.LocatedFlags(cfg, sha, allPaths)
	fotonInputs := in.Inputs
	if corpusPath != "" {
		fotonInputs = append(append([]string{}, in.Inputs...), corpusPath)
	}

	fotonID, err := r.Author(ctx, binaries.AuthorInput{
		Inputs:  fotonInputs,
		Outputs: in.Outputs,
		Cmd:     in.Cmd,
		Located: located,
		SignKey: cfg.PlanktonKey,
	})
	if err != nil {
		return errResult[PublishOutput]("plankton author failed: %v", err)
	}

	outputHashes := map[string]string{}
	for _, o := range in.Outputs {
		h, err := r.Hash(ctx, o)
		if err != nil {
			return errResult[PublishOutput]("plankton hash %s failed: %v", o, err)
		}
		outputHashes[o] = h
	}

	if _, err := gitops.CommitAndPush(ctx, cfg, []string{cfg.Raw.Paths.PlanktonDir}, "foton: "+in.Cmd); err != nil {
		return errResult[PublishOutput]("git commit/push of the registry failed: %v", err)
	}

	permalinks := map[string]string{}
	base := gitops.PermalinkBase(cfg, sha)
	for _, p := range allPaths {
		permalinks[p] = base + "/" + p
	}

	return &mcp.CallToolResult{}, PublishOutput{
		FotonID:      fotonID,
		OutputHashes: outputHashes,
		CommitSHA:    sha,
		Permalinks:   permalinks,
	}, nil
}

func errResult[T any](format string, args ...any) (*mcp.CallToolResult, T, error) {
	var zero T
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}, zero, nil
}

// validatePublishPath rejects a repo-relative path publish must never commit/push: an absolute or
// ..-escaping path, anything that could be parsed as a git command-line flag rather than a literal
// path, anything inside this repo's configured keys_dir, anything inside .git, this repo's own
// cockpit.config.json, or anything with a .key extension regardless of location (a floor of
// protection independent of keys_dir being configured correctly at all — see the "smaller" review
// item that config validation doesn't require keys_dir to be set). Comparisons are
// case-insensitive: this project develops on WSL/NTFS, a case-insensitive filesystem, where e.g.
// "KEYS/x" and "keys/x" name the same file regardless of what a case-sensitive string compare
// would conclude.
//
// Known, deliberately out of scope: this is a PATH check only, never a content check. Copying a
// key's bytes into an innocuously-named file (`cp keys/session-1.key data/results.csv`) and
// publishing that sails through untouched — closing that requires inspecting file content, a
// fundamentally different mechanism than a denylist. This fix closes the path-based hole Michael
// described; content-based exfiltration is a separate, unaddressed risk.
func validatePublishPath(cfg *config.Config, p string) error {
	if p == "" {
		return fmt.Errorf("empty path is not a valid publish path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("%q is an absolute path — publish only accepts repo-relative paths", p)
	}
	// gitops.CommitAndPush passes paths to `git add` with a "--" separator specifically so a path
	// starting with "-" can never be parsed as a flag (see that fix for the full rationale) — this
	// is the second, defense-in-depth layer: reject it here too, on both the raw string actually
	// passed to git and its cleaned form, so this check keeps working even if some future refactor
	// ever dropped the "--" or fed a cleaned path to git instead of the raw one.
	if strings.HasPrefix(p, "-") {
		return fmt.Errorf("%q looks like a command-line flag (starts with -), not a path — refusing", p)
	}

	clean := filepath.ToSlash(filepath.Clean(p))
	if strings.HasPrefix(clean, "-") {
		return fmt.Errorf("%q looks like a command-line flag once cleaned (starts with -) — refusing", p)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%q escapes the repository root (contains ..) — refusing", p)
	}
	// Defense in depth: confirm the cleaned path still resolves inside the repo root, in case some
	// traversal shape slips past the plain ".." prefix check above.
	rel, err := filepath.Rel(cfg.RepoRoot, filepath.Join(cfg.RepoRoot, clean))
	if err != nil {
		return fmt.Errorf("%q could not be resolved against the repository root: %v", p, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%q resolves outside the repository root — refusing", p)
	}

	// Compared against cfg.KeysDir — the already-resolved ABSOLUTE path (config.go joins RepoRoot
	// with raw.Paths.KeysDir) — not the raw cfg.Raw.Paths.KeysDir string. Comparing against the raw
	// value directly would silently no-op if an operator ever wrote keys_dir anchored like "/keys"
	// (config.validate() doesn't forbid it): filepath.Join resolves that correctly into cfg.KeysDir
	// regardless, but the raw string itself stays "/keys" — which, compared as-is against a
	// repo-relative cleaned path, never shares a prefix and never matches.
	absPath := filepath.ToSlash(filepath.Join(cfg.RepoRoot, clean))
	if pathIsUnderOrEqual(absPath, cfg.KeysDir) {
		return fmt.Errorf("%q is inside this repo's configured keys_dir (%s) — publish never touches signing keys", p, cfg.Raw.Paths.KeysDir)
	}
	if pathIsUnderOrEqual(clean, ".git") {
		return fmt.Errorf("%q is inside .git — publish never touches git internals", p)
	}
	if strings.EqualFold(clean, "cockpit.config.json") {
		return fmt.Errorf("%q is this repo's cockpit.config.json — publish never touches its own config", p)
	}
	// TrimRight strips trailing dots/spaces before checking the extension: filepath.Ext("x.key.")
	// is ".", not ".key", so a trailing dot or space would otherwise let a key slip past this
	// check under a name like "session-1.key." or "session-1.key " — exactly the kind of
	// NTFS/Windows-adjacent artifact this project's WSL/NTFS dev environment can actually produce.
	if strings.EqualFold(filepath.Ext(strings.TrimRight(clean, ". ")), ".key") {
		return fmt.Errorf("%q matches *.key — publish never commits signing key material, regardless of location", p)
	}

	return nil
}

// pathIsUnderOrEqual reports whether path is dir itself or somewhere inside it, comparing
// case-insensitively. Both arguments must already be slash-separated and in the same form (both
// repo-relative, or both absolute) — callers are responsible for that; this makes no attempt to
// resolve or normalize further. dir may be empty (an unset config path never matches anything,
// rather than matching everything).
func pathIsUnderOrEqual(path, dir string) bool {
	if dir == "" {
		return false
	}
	pathLower := strings.ToLower(path)
	dirLower := strings.ToLower(filepath.ToSlash(filepath.Clean(dir)))
	return pathLower == dirLower || strings.HasPrefix(pathLower, dirLower+"/")
}
