package tools

import (
	"context"
	"fmt"

	"github.com/deathbychoco/claude-science-cockpit/internal/anchor"
	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
	"github.com/deathbychoco/claude-science-cockpit/internal/material"
	"github.com/deathbychoco/claude-science-cockpit/internal/show"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SayInput is cockpit_say's argument shape — the "sagen" verb: bind a claim, from an allowed
// template, to a foton or file. Claude names a template and supplies field values; for the
// "reproduces" template specifically, the cockpit — not Claude — determines the resulting
// level/reproducedBy fields by actually running the reproduction precondition.
type SayInput struct {
	Subject  string            `json:"subject" jsonschema:"the sha256 foton id or hash the claim is about"`
	Template string            `json:"template" jsonschema:"the claim template name (must be in this repo's allowed-templates config)"`
	Fields   map[string]string `json:"fields,omitempty" jsonschema:"template field values, for templates other than reproduces"`

	// reproduces-only fields: the cockpit computes level/reproducedBy itself from these.
	SubjectOutputHash string `json:"subjectOutputHash,omitempty" jsonschema:"output hash of the producer foton being reproduced (reproduces template only)"`
	ReproducedOutput  string `json:"reproducedOutput,omitempty" jsonschema:"repo-relative path of Claude's own output file that reproduces the subject (reproduces template only)"`
	ReproducedFotonID string `json:"reproducedFotonId,omitempty" jsonschema:"foton id of Claude's own producer foton for reproducedOutput (reproduces template only)"`
	Via               string `json:"via,omitempty" jsonschema:"optional shared normalizer ref, for an L1 (not L0) reproduction"`
}

type SayOutput struct {
	ClaimID      string `json:"claimId"`
	Level        string `json:"level,omitempty"`
	Confirmation string `json:"confirmation"`
	// RekorLogIndex and RekorUUID are set when this repo anchors its records — see PublishOutput.
	RekorLogIndex int64  `json:"rekorLogIndex,omitempty"`
	RekorUUID     string `json:"rekorUuid,omitempty"`
}

func Say(ctx context.Context, _ *mcp.CallToolRequest, in SayInput) (*mcp.CallToolResult, SayOutput, error) {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return errResult[SayOutput]("%v", err)
	}
	if !cfg.Raw.Verbs.Say {
		return errResult[SayOutput]("say is disabled by this repo's cockpit.config.json")
	}
	if !cfg.AllowsTemplate(in.Template) {
		return errResult[SayOutput]("template %q is not in this repo's allowed claim templates", in.Template)
	}
	if in.Subject == "" {
		return errResult[SayOutput]("say requires subject (the foton id or hash the claim is about)")
	}

	r := binaries.New(cfg)
	if err := r.EnsureKernel(ctx); err != nil {
		return errResult[SayOutput]("%v", err)
	}
	sets := in.Fields
	if sets == nil {
		sets = map[string]string{}
	}

	if in.Template == "reproduces" {
		level, err := determineReproductionLevel(ctx, cfg, r, in)
		if err != "" {
			return errResult[SayOutput]("%s", err)
		}
		sets = map[string]string{"level": level, "reproducedBy": in.ReproducedFotonID}
	}

	claimID, err := r.Annotate(ctx, in.Subject, in.Template, sets, cfg.NektonKey)
	if err != nil {
		return errResult[SayOutput]("nekton annotate failed: %v", err)
	}

	// A claim carries the same configured evidence a foton does. A cockpit whose certificate rode
	// only on its fotons would say who produced a result and leave who VOUCHED for it unattributed —
	// and a claim is exactly the record where that question is being asked.
	if err := material.Attach(ctx, cfg, r, claimID, material.Claim); err != nil {
		return errResult[SayOutput]("%v", err)
	}

	var anchored *anchor.Entry
	if cfg.Raw.Anchor.Enabled {
		anchored, err = anchor.Record(ctx, cfg, claimID, anchor.Claim)
		if err != nil {
			return errResult[SayOutput]("the claim was registered but anchoring it failed: %v", err)
		}
	}

	registryPaths := []string{cfg.Raw.Paths.NektonDir}
	if cfg.Raw.Union.Publish {
		written, uerr := show.WriteUnion(ctx, cfg)
		if uerr != nil {
			return errResult[SayOutput]("the claim was registered but publishing the union failed: %v", uerr)
		}
		registryPaths = append(registryPaths, written...)
	}
	if _, err := gitops.CommitAndPush(ctx, cfg, registryPaths, "claim: "+in.Template+" on "+in.Subject); err != nil {
		return errResult[SayOutput]("git commit/push of the claim registry failed: %v", err)
	}

	claims, err := r.About(ctx, in.Subject)
	if err != nil {
		return errResult[SayOutput]("claim was recorded but confirmation query (nekton about) failed: %v", err)
	}
	// The manual workflow confirms a claim by querying it back rather than trusting the write; the
	// cockpit does the same. Until about returned structured claims this could only hand back a
	// block of prose for someone else to read, which meant a claim that signed but never registered
	// still reported success. Now it is an actual check.
	registered := false
	for _, c := range claims {
		if c.ID == claimID {
			registered = true
			break
		}
	}
	if !registered {
		return errResult[SayOutput](
			"claim %s was signed but is not among the %d claim(s) nekton reports about %s — it did not register",
			claimID, len(claims), in.Subject)
	}

	level := sets["level"]
	out := SayOutput{
		ClaimID:      claimID,
		Level:        level,
		Confirmation: fmt.Sprintf("registered: nekton reports %s among the %d claim(s) about %s", claimID, len(claims), in.Subject),
	}
	if anchored != nil {
		out.RekorLogIndex, out.RekorUUID = anchored.LogIndex, anchored.UUID
	}
	return &mcp.CallToolResult{}, out, nil
}

// determineReproductionLevel runs the reproduction precondition itself (never trusting a
// self-declared level from Claude) and returns either the achieved level ("L0"/"L1") or, as
// its second return value, a non-empty error message.
func determineReproductionLevel(ctx context.Context, cfg *config.Config, r *binaries.Runner, in SayInput) (level string, errMsg string) {
	if in.SubjectOutputHash == "" || in.ReproducedOutput == "" || in.ReproducedFotonID == "" {
		return "", "reproduces requires subjectOutputHash, reproducedOutput, and reproducedFotonId"
	}

	candHash, err := r.Hash(ctx, in.ReproducedOutput)
	if err != nil {
		return "", "plankton hash of reproducedOutput failed: " + err.Error()
	}

	via := in.Via
	if via == "" {
		via = cfg.Raw.Reproduction.Normalizer
	}

	verdict, err := r.Reproduces(ctx, in.SubjectOutputHash, candHash, via)
	if err != nil {
		return "", "plankton reproduces failed: " + err.Error()
	}
	if !verdict.Matched {
		return "", fmt.Sprintf(
			"reproduction precondition failed — the outputs do not match: %s does not reproduce %s",
			candHash, in.SubjectOutputHash)
	}
	// The level is plankton's own answer, read from a named field. It is never inferred from whether
	// --via was passed: byte-identical outputs are L0 even when a normalizer was offered, since
	// plankton checks ref == cand before consulting one — so inferring "L1 whenever via != \"\""
	// mislabels a genuine L0 in any repo with a default normalizer, and an L0 policy then rejects a
	// reproduction that was correct.
	achieved := verdict.Level
	if achieved == "" {
		return "", "plankton reported a match without a level, which is not an answer this can record"
	}

	if cfg.Raw.Reproduction.RequiredLevel == "L0" && achieved != "L0" {
		return "", fmt.Sprintf("this repo's policy requires L0; this reproduction only reached %s", achieved)
	}
	return achieved, ""
}
