package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/anchor"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/container"
	"github.com/kton-protocol/kton-cockpit/internal/gitops"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"github.com/kton-protocol/kton-cockpit/internal/show"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PublishInput is cockpit_publish's argument shape — the "veröffentlichen" verb. The caller supplies
// plain repo-relative paths and the command that was run; the cockpit owns every permalink,
// commit, and signature.
type PublishInput struct {
	Inputs  []string `json:"inputs" jsonschema:"repo-relative paths this computation consumed"`
	Outputs []string `json:"outputs,omitempty" jsonschema:"repo-relative paths this computation produced"`
	// OutputDir names a directory whose contents after the run ARE the outputs, instead of naming
	// them. It is only meaningful when this repo executes the command, because only then does the
	// cockpit know what the run produced rather than what the caller believed it would.
	//
	// The directory MUST be empty or absent beforehand, and publish refuses otherwise. That is what
	// makes the result deterministic, and it is the whole reason this is safe where "take everything
	// that changed" is not: output paths and hashes are COVERED, so a stale file left from an
	// earlier run would enter this record's identity and two identical runs would stop producing
	// the same foton. An empty dedicated directory has no such file in it.
	OutputDir string `json:"outputDir,omitempty" jsonschema:"a directory whose contents after the run are the outputs; requires execution, and must be empty beforehand"`
	Cmd       string `json:"cmd" jsonschema:"the exact command that was run to produce the outputs"`
	// Corpus lists nekton claim or plankton foton refs (sha256:...) that this result's own
	// reasoning drew on as its basis. When set, the cockpit records a small corpus manifest file
	// as an extra foton input, so the reasoning-as-basis is itself a registered foton whose
	// inputs are the touched nektons — never a silent re-attestation ("Lake bleibt Lake").
	Corpus []string `json:"corpus,omitempty" jsonschema:"nekton claim or plankton foton refs (sha256:...) this reasoning drew on as its basis, if any"`
	// EnvRef names the execution environment this work actually ran in — the container image, nix
	// store path, or run-server id. It is per-call because it varies per call: a session can work
	// across many containers, so no configured value could be right for all of them.
	//
	// Like Cmd, it is attested rather than proven. A foton is a signed statement — "I ran this
	// command, in this environment, on these inputs, producing these outputs" — where the hashes
	// make inputs and outputs checkable and the command and environment are what the signer vouches
	// for. plankton says as much of the command: it RECORDS it, and never runs it. Treating the
	// command that way and the environment differently would be an inconsistency, not a safeguard.
	EnvRef string `json:"envRef,omitempty" jsonschema:"the execution environment this ran in, digest-pinned: oci://<image>@sha256:<digest>, a nix store path, or a run-server id"`
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
	// Environment reports which execution environment this foton pinned, if any.
	Environment string `json:"environment,omitempty"`
	EnvRef      string `json:"envRef,omitempty"`
	// ExecutedIn names the image the cockpit ran the command in, when this repo is configured to
	// run rather than record. Empty means the command was run elsewhere and only recorded here —
	// in which case envRef, if set, is the operator's assertion rather than an observation.
	ExecutedIn string `json:"executedIn,omitempty"`
	// NetworkAllowed reports that the run was NOT network-isolated. Surfaced deliberately: such a
	// run depended on something the foton does not pin, so its environment claim is weaker, and
	// whoever reads the result should see that rather than have to know the repo's config.
	NetworkAllowed bool `json:"networkAllowed,omitempty"`
	// Stdout is what the command printed, when the cockpit ran it.
	Stdout string `json:"stdout,omitempty"`
	// UnionPublished reports that this repo's aggregate was regenerated and committed alongside the
	// record, so the graph is reachable online without running `cockpit show`.
	UnionPublished bool `json:"unionPublished,omitempty"`
	// UndeclaredChanges lists files the run changed that this publish did not name as an input or an
	// output — reported only when the cockpit ran the command, since otherwise it has no way to know.
	//
	// Not an error: a temp file or a cache is legitimate, and a foton is not meant to describe every
	// byte a machine touched. But an OUTPUT produced and not declared is invisible without this —
	// the foton understates the work, nothing fails, and the omission surfaces much later as a chain
	// that does not join. It is also why the outputs are not simply taken from what changed: they
	// are COVERED, so incidental files would enter the foton's identity and two identical runs would
	// stop producing the same record.
	UndeclaredChanges []string `json:"undeclaredChanges,omitempty"`
	// RekorLogIndex and RekorUUID are where an independent witness recorded that this exact record
	// existed by that time. Present only when this repo anchors: a signature says who signed, not
	// when, and not that the signer did not later prefer a different record.
	RekorLogIndex int64  `json:"rekorLogIndex,omitempty"`
	RekorUUID     string `json:"rekorUuid,omitempty"`
	// PushRejected means the record is committed here and not yet anywhere else — somebody pushed
	// between this run starting and finishing. Nothing is lost: `git pull --rebase && git push`.
	// Reported as its own field rather than as pushed:false, because "this repo does not push" and
	// "this push was refused" call for different things from whoever reads it.
	PushRejected bool `json:"pushRejected,omitempty"`
	// Committed and Pushed say what actually happened to git, because both are configurable and
	// each changes what the permalinks are worth. Not committed means there are none: the sha one
	// would pin does not exist. Committed but not pushed means they are correct and will resolve
	// once someone pushes — but do not yet.
	Committed bool `json:"committed"`
	Pushed    bool `json:"pushed"`
}

func Publish(ctx context.Context, _ *mcp.CallToolRequest, in PublishInput) (*mcp.CallToolResult, PublishOutput, error) {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return errResult[PublishOutput]("%v", err)
	}
	if !cfg.Raw.Verbs.Publish {
		return errResult[PublishOutput]("publish is disabled by this repo's cockpit.config.json")
	}
	switch {
	case in.OutputDir != "" && len(in.Outputs) > 0:
		return errResult[PublishOutput]("publish takes either outputs or outputDir, not both")
	case in.OutputDir != "" && !cfg.Raw.Execution.Enabled():
		return errResult[PublishOutput](
			"outputDir needs this repo to run the command (execution.image is not configured) — " +
				"without that the cockpit never sees the run and cannot know what it produced")
	case in.OutputDir == "" && len(in.Outputs) == 0:
		return errResult[PublishOutput]("publish requires at least one output path, or outputDir")
	}
	if in.Cmd == "" {
		return errResult[PublishOutput]("publish requires cmd (the command that produced the outputs)")
	}

	envRef, envErr := resolveEnvRef(cfg, in.EnvRef)
	if envErr != "" {
		return errResult[PublishOutput]("%s", envErr)
	}

	r := binaries.New(cfg)
	allPaths := append(append([]string{}, in.Inputs...), in.Outputs...)

	// a session only gets three narrow verbs; the cockpit owns key hygiene — but without this check,
	// publish is a general "commit and push any path in this repo" primitive, since git itself
	// doesn't care whether a staged path is a signing key. Validate every input/output path BEFORE
	// touching git or plankton at all, and report every denied path at once (not just the first),
	// so a retry can fix everything in one pass.
	//
	// It runs before the container too, and that ordering is the point once this repo executes: a
	// run is the first thing that touches anything, so a path denied here is denied before a
	// command has had the chance to act on it.
	var denied []string
	for _, p := range allPaths {
		if verr := validatePublishPath(cfg, p); verr != nil {
			denied = append(denied, verr.Error())
		}
	}
	if len(denied) > 0 {
		return errResult[PublishOutput]("publish refused %d path(s):\n%s", len(denied), strings.Join(denied, "\n"))
	}

	// When this repo runs rather than records, the command executes BEFORE anything is committed:
	// the outputs do not exist until it has. A failed run is a failed publish — a foton describing
	// whatever a failed run left behind would assert work that never completed.
	var ran *container.Result
	var undeclared []string
	if in.OutputDir != "" {
		existing, derr := filesUnder(cfg.RepoRoot, in.OutputDir)
		if derr != nil {
			return errResult[PublishOutput]("outputDir %s: %v", in.OutputDir, derr)
		}
		if len(existing) > 0 {
			return errResult[PublishOutput](
				"outputDir %s is not empty (%d file(s), e.g. %s) — empty it first.\n"+
					"A file left there by an earlier run would become an output of THIS record, and "+
					"output paths and hashes are part of a foton's identity, so the same work would "+
					"stop producing the same record.", in.OutputDir, len(existing), existing[0])
		}
	}

	if cfg.Raw.Execution.Enabled() {
		// Snapshotted around the run so the declaration can be held against what actually happened.
		// This is the only point where that is possible: when the cockpit does not run the command,
		// it has no idea what the command touched.
		before, serr := container.TakeSnapshot(cfg.RepoRoot)
		if serr != nil {
			return errResult[PublishOutput]("could not read the working tree before the run: %v", serr)
		}

		ran, err = container.Run(ctx, cfg, in.Cmd)
		if err != nil {
			return errResult[PublishOutput]("%v", err)
		}
		if in.OutputDir != "" {
			produced, derr := filesUnder(cfg.RepoRoot, in.OutputDir)
			if derr != nil {
				return errResult[PublishOutput]("reading outputDir %s after the run: %v", in.OutputDir, derr)
			}
			if len(produced) == 0 {
				return errResult[PublishOutput](
					"the command succeeded in %s but wrote nothing to %s — a record of a run that "+
						"produced no output asserts work with no result",
					cfg.Raw.Execution.Image, in.OutputDir)
			}
			in.Outputs = produced
		}
		for _, o := range in.Outputs {
			if _, statErr := os.Stat(filepath.Join(cfg.RepoRoot, o)); statErr != nil {
				return errResult[PublishOutput](
					"the command succeeded in %s but produced no %s — publish declares outputs that must exist afterwards",
					cfg.Raw.Execution.Image, o)
			}
		}

		after, serr := container.TakeSnapshot(cfg.RepoRoot)
		if serr != nil {
			return errResult[PublishOutput]("could not read the working tree after the run: %v", serr)
		}
		undeclared = before.ChangedSince(after, append(append([]string{}, in.Inputs...), in.Outputs...))
	}

	// A corpus entry is a record this result STANDS ON, so this repo's bar for standing on someone
	// else's work applies here and nowhere else. Checked before anything is written: a publish that
	// failed this after committing would leave the basis recorded and the conclusion refused.
	if len(in.Corpus) > 0 && cfg.Raw.Reproduction.MinReproductions > 0 {
		if msg := checkCorpusCorroboration(ctx, cfg, r, in.Corpus); msg != "" {
			return errResult[PublishOutput]("%s", msg)
		}
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

	// A rejected push is NOT a failed publish. The commit is made, its sha is real, and the
	// permalinks built from it will resolve the moment somebody pushes — so the record is still
	// worth writing, and throwing it away also throws away the container run that produced it.
	pushRejected := false
	sha, err := gitops.CommitAndPush(ctx, cfg, allPaths, "publish: "+in.Cmd)
	if err != nil {
		var pf *gitops.PushFailed
		if !errors.As(err, &pf) {
			return errResult[PublishOutput]("git commit of inputs+outputs failed: %v", err)
		}
		pushRejected, sha = true, pf.SHA
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
		// From the config, never from the caller: both values are COVERED, so a self-declared one
		// would bake an unverified assertion about which environment ran into the record's own
		// identity. See config.Environment.
		Environment: cfg.Raw.Environment.Spectrum,
		EnvRef:      envRef,
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

	// Configured evidence is attached before anything is committed, for the same reason the anchor
	// is: material binds to the record's content address and is stored beside it, so a record and
	// the evidence about it belong in one commit. A later commit could be missing from a partial
	// share, and the record would then arrive stripped of what was meant to travel with it.
	if err := material.Attach(ctx, cfg, r, fotonID, material.Foton); err != nil {
		return errResult[PublishOutput]("%v", err)
	}

	// Anchored before the registry commit, so the proof travels with the record it witnesses rather
	// than in a later commit that could be missing from a partial share.
	var anchored *anchor.Entry
	if cfg.Raw.Anchor.Enabled {
		anchored, err = anchor.Record(ctx, cfg, fotonID, anchor.Foton)
		if err != nil {
			return errResult[PublishOutput]("the foton was registered but anchoring it failed: %v", err)
		}
	}

	// The union is regenerated AFTER the foton is in the registry and committed in the same commit
	// as it, so a published aggregate is never one record behind the registry it summarises.
	registryPaths := []string{cfg.Raw.Paths.PlanktonDir}
	if cfg.Raw.Union.Publish {
		written, uerr := show.WriteUnion(ctx, cfg)
		if uerr != nil {
			return errResult[PublishOutput]("the foton was registered but publishing the union failed: %v", uerr)
		}
		registryPaths = append(registryPaths, written...)
	}

	// Publish does two SEPARATE commits — inputs+outputs first, then the signed registry entry,
	// since the foton can't be authored until its --located permalinks (anchored to the FIRST
	// commit) already exist. This second commit's sha is what the call returns: the first was
	// always one commit stale by the time Publish came back, and `git ls-remote origin HEAD`
	// disagreed with it. The foton's OWN embedded --located permalinks stay anchored to the first
	// commit — unavoidable, since authoring happens between the two — but the bytes are identical
	// in both, so they resolve either way.
	finalSHA, err := gitops.CommitAndPush(ctx, cfg, registryPaths, "foton: "+in.Cmd)
	if err != nil {
		var pf *gitops.PushFailed
		if errors.As(err, &pf) {
			pushRejected, finalSHA, err = true, pf.SHA, nil
		}
	}
	if err != nil {
		return errResult[PublishOutput]("git commit/push of the registry failed: %v", err)
	}

	// finalSHA is empty when this repo does not commit: there is then no commit for a permalink to
	// pin, and returning HEAD instead would be worse than returning nothing — a locator pinned to a
	// commit that does not contain the bytes resolves to the wrong thing or to nothing.
	permalinks := map[string]string{}
	if finalSHA != "" {
		base := gitops.PermalinkBase(cfg, finalSHA)
		for _, p := range allPaths {
			permalinks[p] = base + "/" + p
		}
	}

	out := PublishOutput{
		FotonID:        fotonID,
		OutputHashes:   outputHashes,
		CommitSHA:      finalSHA,
		Permalinks:     permalinks,
		Environment:    cfg.Raw.Environment.Spectrum,
		EnvRef:         envRef,
		Committed:      cfg.Raw.CommitEnabled(),
		Pushed:         cfg.Raw.PushEnabled() && !pushRejected,
		PushRejected:   pushRejected,
		UnionPublished: cfg.Raw.Union.Publish,
	}
	out.UndeclaredChanges = undeclared
	if anchored != nil {
		out.RekorLogIndex, out.RekorUUID = anchored.LogIndex, anchored.UUID
	}
	if ran != nil {
		out.ExecutedIn = ran.Image
		out.NetworkAllowed = cfg.Raw.Execution.Network
		out.Stdout = ran.Stdout
	}
	return &mcp.CallToolResult{}, out, nil
}

// resolveEnvRef decides which execution environment this publish records.
//
// The caller names it, because only the caller knows: a session can work across many containers, so
// no configured value could be right for all of them. It is a claim, like the inputs, the outputs
// and the command already are — the caller names those too, and the cockpit only hashes the files it is
// pointed at. An omitted input would be a more consequential false statement than a wrong envRef,
// and nothing prevents that either. What makes any of it trustworthy is the signature, not a check
// the cockpit could not perform anyway.
//
// The one case the cockpit does not take a claim about is the one where it knows: when it ran the
// container itself, what it ran in is not open to argument.
//
// The second return value is a non-empty error message.
func resolveEnvRef(cfg *config.Config, supplied string) (envRef string, errMsg string) {
	if supplied != "" {
		if err := config.ValidateEnvRef(supplied); err != nil {
			return "", err.Error()
		}
	}
	if cfg.Raw.Execution.Enabled() {
		if supplied != "" && supplied != cfg.Raw.Execution.Image {
			return "", fmt.Sprintf(
				"envRef %s was supplied, but this repo runs the command itself in %s — the cockpit records "+
					"what it ran in, and will not record something else", supplied, cfg.Raw.Execution.Image)
		}
		return cfg.Raw.Execution.Image, ""
	}
	// Otherwise the call wins and the config is only a default, for repos whose environment nobody
	// names per call.
	if supplied != "" {
		return supplied, ""
	}
	return cfg.Raw.Environment.EnvRef, ""
}

// checkCorpusCorroboration requires every record the caller names as a basis to have been produced
// independently at least minReproductions times, counted by verified signature.
//
// It asks about the record's OUTPUT bytes rather than the record itself, because that is what
// reproduction means here: another party ran the work and arrived at the same bytes. Two signatures
// on one foton is exactly that (kton §6.3) — identical work is the same record, and the count is of
// distinct verified signers.
//
// The second return value is a non-empty error message.
func checkCorpusCorroboration(ctx context.Context, cfg *config.Config, r *binaries.Runner, corpus []string) string {
	want := cfg.Raw.Reproduction.MinReproductions
	for _, ref := range corpus {
		res, err := r.Reproductions(ctx, ref, "")
		if err != nil {
			return fmt.Sprintf(
				"this repo requires %d independent reproduction(s) before a record may be the basis of its "+
					"own work, and the count for %s could not be established: %v", want, ref, err)
		}
		if res.DistinctSigners < want {
			excluded := ""
			if res.ExcludedUntrusted > 0 {
				// The difference between "nobody reproduced this" and "somebody did, but nobody this
				// repo trusts" is the one a reader needs to act on.
				excluded = fmt.Sprintf(" (%d further producer(s) were signed by no key this repo trusts)",
					res.ExcludedUntrusted)
			}
			return fmt.Sprintf(
				"%s has %d verified independent reproduction(s)%s, and this repo requires %d before a "+
					"record may be the basis of its own work",
				ref, res.DistinctSigners, excluded, want)
		}
	}
	return ""
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

// filesUnder lists the repo-relative paths of every file below dir, sorted, or an empty slice if
// dir does not exist. Sorted because output order is COVERED: the same run must produce the same
// foton, and a directory walk is not obliged to return the same order twice.
func filesUnder(repoRoot, dir string) ([]string, error) {
	root := filepath.Join(repoRoot, filepath.FromSlash(dir))
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file, not a directory", dir)
	}
	var out []string
	werr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if werr != nil {
		return nil, werr
	}
	sort.Strings(out)
	return out, nil
}
