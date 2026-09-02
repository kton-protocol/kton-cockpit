package tools

import (
	"context"
	"fmt"
	"regexp"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/gitops"
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
	return &mcp.CallToolResult{}, SayOutput{
		ClaimID:      claimID,
		Level:        level,
		Confirmation: fmt.Sprintf("registered: nekton reports %s among the %d claim(s) about %s", claimID, len(claims), in.Subject),
	}, nil
}

// reproductionLevelRe matches plankton's own `reproduction: <level>` line — printed at the start
// of a line whether the match was identical bytes (L0) or a --via normalizer match (L1); see
// reproductionLevelRe's use in determineReproductionLevel for why this is parsed rather than
// inferred from whether --via was passed. `plankton reproduces` only ever emits L0 or L1 on this
// line (L2 is a comparator's signed verdict, not a kernel check, and is never printed here) — the
// character class is deliberately just L[01], not L[012], so an L2 (or anything else) this command
// was never supposed to print fails closed as unparseable rather than being silently accepted.
var reproductionLevelRe = regexp.MustCompile(`(?m)^reproduction: (L[01])\b`)

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

	ok, out, err := r.Reproduces(ctx, in.SubjectOutputHash, candHash, via)
	if err != nil {
		return "", "plankton reproduces failed: " + err.Error()
	}
	if !ok {
		return "", "reproduction precondition failed — outputs do not match:\n" + out
	}

	// The achieved level comes from plankton's own printed answer, never from whether --via was
	// passed: byte-identical outputs are L0 even when a normalizer was requested (plankton checks
	// ref == cand BEFORE ever consulting --via — see the reference source's `reproduces` case), so
	// inferring "L1 whenever via != \"\"" mislabels a genuine L0 as L1 for any repo that configures
	// a default normalizer, and a mislabelled L1 then fails an L0 policy outright even though the
	// underlying reproduction was correct.
	m := reproductionLevelRe.FindStringSubmatch(out)
	if m == nil {
		return "", "plankton reported success but its reproduction level could not be parsed from its output:\n" + out
	}
	achieved := m[1]

	if cfg.Raw.Reproduction.RequiredLevel == "L0" && achieved != "L0" {
		return "", fmt.Sprintf("this repo's policy requires L0; this reproduction only reached %s", achieved)
	}
	return achieved, ""
}
