package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/anchor"
	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/container"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
	"github.com/deathbychoco/claude-science-cockpit/internal/show"
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
	if len(in.Outputs) == 0 {
		return errResult[PublishOutput]("publish requires at least one output path")
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

	// When this repo runs rather than records, the command executes BEFORE anything is committed:
	// the outputs do not exist until it has. A failed run is a failed publish — a foton describing
	// whatever a failed run left behind would assert work that never completed.
	var ran *container.Result
	var undeclared []string
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
		// From the config, never from Claude: both values are COVERED, so a self-declared one
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
	if _, err := gitops.CommitAndPush(ctx, cfg, registryPaths, "foton: "+in.Cmd); err != nil {
		return errResult[PublishOutput]("git commit/push of the registry failed: %v", err)
	}

	permalinks := map[string]string{}
	if sha != "" {
		base := gitops.PermalinkBase(cfg, sha)
		for _, p := range allPaths {
			permalinks[p] = base + "/" + p
		}
	}

	out := PublishOutput{
		FotonID:        fotonID,
		OutputHashes:   outputHashes,
		CommitSHA:      sha,
		Permalinks:     permalinks,
		Environment:    cfg.Raw.Environment.Spectrum,
		EnvRef:         envRef,
		Committed:      cfg.Raw.CommitEnabled(),
		Pushed:         cfg.Raw.PushEnabled(),
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
// and the command already are — Claude names those too, and the cockpit only hashes the files it is
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

func errResult[T any](format string, args ...any) (*mcp.CallToolResult, T, error) {
	var zero T
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}, zero, nil
}
