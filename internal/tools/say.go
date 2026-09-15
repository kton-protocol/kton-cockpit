package tools

import (
	"context"
	"fmt"
	"sort"
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

	// Scope names one of the scopes this repo configures, chaining the claim into that
	// conversation. Omitted, the claim stands on its own — which is the common case. The name must
	// be one of claims.scopes: the operator fixes the set, exactly as with the template ceiling.
	Scope string `json:"scope,omitempty" jsonschema:"optional: the name of a configured claim scope to chain this claim into"`
}

type SayOutput struct {
	ClaimID      string `json:"claimId"`
	Level        string `json:"level,omitempty"`
	Confirmation string `json:"confirmation"`
	// RekorLogIndex and RekorUUID are set when this repo anchors its records — see PublishOutput.
	RekorLogIndex int64  `json:"rekorLogIndex,omitempty"`
	RekorUUID     string `json:"rekorUuid,omitempty"`
	// Chain is where this claim joined its scope, present only when one is configured. It reports
	// what this registry could see at the time and judges none of it: whether the chain is whole is
	// a seal-verification question, answered by `cockpit_ask` with query "scope".
	Chain *ChainPosition `json:"chain,omitempty"`
}

// ChainPosition is where a claim landed in its scope, and what the substrate saw when it did.
type ChainPosition struct {
	Scope string `json:"scope"`
	// Prev is the statement this claim follows.
	Prev string `json:"prev"`
	// Length is how many claims the scope held before this one.
	Length int `json:"length"`
	// Heads is every head the substrate reported. More than one means claims already shared a prev,
	// so no single head seals the whole — worth seeing, and not a reason to withhold a true claim.
	Heads []string `json:"heads,omitempty"`
	// Unresolved counts claims naming this scope whose prev this registry does not hold. kton §7.4
	// expects this: the missing statement may live in another source, and adding a source can only
	// resolve more. It says the view was partial, not that anything is wrong.
	Unresolved int `json:"unresolved,omitempty"`
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

	chain, head, cerr := resolveChain(ctx, r, cfg, in.Scope)
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
	if chain != nil {
		out.Chain = &ChainPosition{
			Scope: chain.Scope, Prev: chain.Prev,
			Length: head.ChainLength, Heads: head.Heads, Unresolved: head.Unresolved,
		}
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

// resolveChain places this claim after the last statement of the configured scope that this
// registry holds, or returns nil when no scope is configured — the default, which leaves every
// claim standing on its own.
//
// It judges nothing about the chain's completeness, and that is the specification's rule rather
// than a preference. kton §7.4 makes ingest MONOTONE: a well-formed signed scoped statement is
// accepted "even if its `prev` is not yet resolvable (it may live in another source)", and the
// closed-world guarantee — that the chain reaches the seed without a gap — is a SEAL-VERIFICATION
// judgment over the resolved union of sources, evaluated when the seal is relied upon. kton §11 then
// says it outright: a reader MUST NOT generalize that closed-world rule to the open substrate.
//
// An earlier version of this function did exactly that, refusing to write when the scope was
// branched or held a claim whose prev was missing. Both refusals were wrong twice over: they
// generalized the sealed-world rule to ingest, and — because a scope id is public and anyone whose
// records reach this registry can name it — they handed anyone this repo mirrors from a veto over
// its own work. One claim from a key in no configured tier was enough to freeze writing.
//
// Where completeness IS judged is the read path: `cockpit_ask` with query "scope".
func resolveChain(ctx context.Context, r *binaries.Runner, cfg *config.Config, name string) (*binaries.Chain, *binaries.ScopeHead, string) {
	if name == "" {
		return nil, nil, ""
	}
	scope, configured, ok := cfg.ScopeID(name)
	if !ok {
		if len(configured) == 0 {
			return nil, nil, fmt.Sprintf(
				"this repo configures no claim scopes, so %q cannot be one of them. A scope is opened by an "+
					"operator (`nekton seed <name> --sign <key> --add --print-id`) and listed in claims.scopes; "+
					"a claim written without a scope stands on its own.", name)
		}
		// Named rather than matched as empty, for the reason SPEC §9.2 refuses an unknown filter value: a
		// typo that quietly wrote somewhere else, or nowhere, reads like a decision.
		return nil, nil, fmt.Sprintf("no claim scope named %q in this repo's config; it has: %s",
			name, strings.Join(configured, ", "))
	}
	head, err := r.Head(ctx, scope)
	if err != nil {
		return nil, nil, fmt.Sprintf(
			"claims.scope names %s, but this repo's nekton registry cannot report its head: %v\n"+
				"A scope must be seeded and ingested here before claims can chain under it "+
				"(`nekton seed <name> --sign <key> --add`).", scope, err)
	}
	// With several heads the substrate's ordering is not a decision, so the pick is made
	// deterministic: the same work run twice lands on the same prev rather than on whichever head
	// came back first. Which head the scope's order should really follow is a seal-verification
	// question, and it is answered where seals are — on the read path, not here.
	heads := append([]string(nil), head.Heads...)
	sort.Strings(heads)
	return &binaries.Chain{Scope: scope, Prev: heads[0]}, head, ""
}
