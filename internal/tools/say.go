package tools

import (
	"context"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
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

	if _, err := gitops.CommitAndPush(ctx, cfg, []string{cfg.Raw.Paths.NektonDir}, "claim: "+in.Template+" on "+in.Subject); err != nil {
		return errResult[SayOutput]("git commit/push of the claim registry failed: %v", err)
	}

	confirmation, err := r.About(ctx, in.Subject)
	if err != nil {
		return errResult[SayOutput]("claim was recorded but confirmation query (nekton about) failed: %v", err)
	}

	level := sets["level"]
	return &mcp.CallToolResult{}, SayOutput{ClaimID: claimID, Level: level, Confirmation: confirmation}, nil
}

// determineReproductionLevel runs the reproduction precondition itself (never trusting a
// self-declared level from Claude) and returns either the achieved level ("L0"/"L1") or, as its
// second return value, a non-empty error message.
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

	ok, out, err := r.Reproduces(ctx, in.SubjectOutputHash, candHash, via)
	if err != nil {
		return "", "plankton reproduces failed: " + err.Error()
	}
	if !ok {
		return "", "reproduction precondition failed — outputs do not match:\n" + out
	}

	achieved := "L0"
	if via != "" {
		achieved = "L1"
	}
	if cfg.Raw.Reproduction.RequiredLevel == "L0" && achieved != "L0" {
		return "", "this repo's policy requires L0; this reproduction only reached L1 via a normalizer"
	}
	return achieved, ""
}
