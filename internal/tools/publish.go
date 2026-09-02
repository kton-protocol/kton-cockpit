package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Environment reports which execution environment this foton pinned, if any. Reported because
	// it is otherwise invisible: it is covered by the foton id, but `plankton show` prints only the
	// descriptor's cmd, so nothing downstream displays it.
	Environment string `json:"environment,omitempty"`
	EnvRef      string `json:"envRef,omitempty"`
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
		EnvRef:      cfg.Raw.Environment.EnvRef,
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
		Environment:  cfg.Raw.Environment.Spectrum,
		EnvRef:       cfg.Raw.Environment.EnvRef,
	}, nil
}

func errResult[T any](format string, args ...any) (*mcp.CallToolResult, T, error) {
	var zero T
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}, zero, nil
}
