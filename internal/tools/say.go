package tools

import (
	"context"
	"fmt"
	"strings"

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

	chain, cerr := resolveChain(ctx, r, cfg)
	if cerr != "" {
		return errResult[SayOutput]("%s", cerr)
	}

	claimID, err := r.Annotate(ctx, in.Subject, in.Template, sets, cfg.NektonKey, chain)
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

// resolveChain decides where in a configured scope's chain this claim goes, or that it must not be
// written at all. It returns (nil, "") when no scope is configured, which is the default and leaves
// every claim standing on its own.
//
// The tip is read from the substrate on each call rather than remembered. Two conditions make it
// unusable, and both are refusals rather than choices:
//
//   - BRANCHED. Claims already share a prev, so each head commits only to its own branch. The
//     kernel reports the structure and deliberately prescribes no remedy (kton §7.4 leaves sealing
//     rules to consumers), so the choice lands here — and picking one silently would make which
//     branch a claim belongs to depend on which session happened to run first. Someone has to
//     decide which branch this review IS; that someone is not a tool with no view of the work.
//
//   - UNRESOLVED. A claim names this scope and its prev is not held here, so a withheld MIDDLE
//     claim leaves its successors unreachable and the reported tip is provisional. This is the
//     worse of the two: chaining onto a provisional tip does not inherit a fork, it CREATES one
//     the moment the missing claims arrive. Fetch them first.
func resolveChain(ctx context.Context, r *binaries.Runner, cfg *config.Config) (*binaries.Chain, string) {
	scope := cfg.Raw.Claims.Scope
	if scope == "" {
		return nil, ""
	}
	head, err := r.Head(ctx, scope)
	if err != nil {
		return nil, fmt.Sprintf(
			"claims.scope names %s, but this repo's nekton registry cannot report its head: %v\n"+
				"A scope must be seeded and ingested here before claims can chain under it "+
				"(`nekton seed <name> --sign <key> --add`).", scope, err)
	}
	if head.Unresolved > 0 {
		return nil, fmt.Sprintf(
			"scope %s has %d claim(s) whose prev this registry does not hold, so the tip it reports "+
				"(%s) is PROVISIONAL, not the chain's real head. Chaining onto it would not inherit a "+
				"fork — it would create one as soon as the missing claims arrive. Obtain them first.",
			scope, head.Unresolved, head.Heads[0])
	}
	if head.Branched {
		return nil, fmt.Sprintf(
			"scope %s is BRANCHED into %d heads (%s), so each head commits only to the claims on its "+
				"own branch. Which branch this claim belongs to is a judgement about the work, not "+
				"something to pick by running order — the substrate leaves it open and so does this. "+
				"Resolve the branch, then say it again.",
			scope, len(head.Heads), strings.Join(head.Heads, ", "))
	}
	return &binaries.Chain{Scope: scope, Prev: head.Heads[0]}, ""
}
