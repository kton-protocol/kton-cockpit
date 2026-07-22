package tools

import (
	"context"
	"fmt"
	"regexp"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/verify"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AskInput is cockpit_ask's argument shape — the "fragen" verb: query the graph. Filter can only
// narrow within the trust tiers configured for this repo; it can never surface a tier or signer
// absent from cockpit.config.json.
type AskInput struct {
	Query  string     `json:"query" jsonschema:"one of: producer, uses, lineage, reproductions, about, by"`
	Ref    string      `json:"ref" jsonschema:"the hash, subject, or value to query"`
	Filter *AskFilter `json:"filter,omitempty"`
}

type AskFilter struct {
	TrustTier string `json:"trustTier,omitempty" jsonschema:"only include records verified against this configured trust tier"`
}

// RecordVerification is what the record actually verified as — resolved from the verifying key,
// never from the record's own declared keyid/by field.
type RecordVerification struct {
	ID       string `json:"id"`
	Tier     string `json:"tier"`
	Verified bool   `json:"verified"`
}

type AskOutput struct {
	Query string `json:"query"`
	Ref   string `json:"ref"`
	// Raw is the underlying plankton/nekton CLI output, kept for human/debugging context. It is
	// NOT pre-filtered — callers must use Included, not Raw, to decide what to treat as trusted.
	Raw           string                `json:"raw"`
	Records       []RecordVerification  `json:"records"`
	Included      []string              `json:"included"`
	Excluded      []string              `json:"excluded"`
	FilterApplied string                `json:"filterApplied"`
}

var recordIDRe = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

func Ask(ctx context.Context, _ *mcp.CallToolRequest, in AskInput) (*mcp.CallToolResult, AskOutput, error) {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return errResult[AskOutput]("%v", err)
	}
	if !cfg.Raw.Verbs.Ask {
		return errResult[AskOutput]("ask is disabled by this repo's cockpit.config.json")
	}
	if in.Ref == "" {
		return errResult[AskOutput]("ask requires ref (the hash, subject, or value to query)")
	}

	r := binaries.New(cfg)

	var raw string
	var kind verify.Kind
	switch in.Query {
	case "producer":
		raw, err = r.Producer(ctx, in.Ref)
		kind = verify.Foton
	case "uses":
		raw, err = r.Uses(ctx, in.Ref)
		kind = verify.Foton
	case "lineage":
		raw, err = r.Lineage(ctx, in.Ref)
		kind = verify.Foton
	case "reproductions":
		raw, err = r.Reproductions(ctx, in.Ref)
		kind = verify.Foton
	case "about":
		raw, err = r.About(ctx, in.Ref)
		kind = verify.Claim
	case "by":
		raw, err = r.By(ctx, in.Ref)
		kind = verify.Claim
	default:
		return errResult[AskOutput]("unknown query %q (must be one of: producer, uses, lineage, reproductions, about, by)", in.Query)
	}
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	ids := uniqueMatches(recordIDRe, raw)
	records := make([]RecordVerification, 0, len(ids))
	included := []string{}
	excluded := []string{}

	wantTier := ""
	if in.Filter != nil {
		wantTier = in.Filter.TrustTier
	}

	for _, id := range ids {
		tier, _, verr := verify.ResolveTier(ctx, r, cfg, id, kind)
		if verr != nil {
			return errResult[AskOutput]("verifying %s failed: %v", id, verr)
		}
		verified := tier != verify.Untrusted
		records = append(records, RecordVerification{ID: id, Tier: tier, Verified: verified})

		include := verified
		if include && wantTier != "" {
			include = tier == wantTier
		}
		if include {
			included = append(included, id)
		} else {
			excluded = append(excluded, id)
		}
	}

	filterApplied := "none — every record verified against a configured trust tier is included; unverified records are always excluded"
	if wantTier != "" {
		filterApplied = fmt.Sprintf("trustTier=%s", wantTier)
	}

	return &mcp.CallToolResult{}, AskOutput{
		Query:         in.Query,
		Ref:           in.Ref,
		Raw:           raw,
		Records:       records,
		Included:      included,
		Excluded:      excluded,
		FilterApplied: filterApplied,
	}, nil
}

func uniqueMatches(re *regexp.Regexp, s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(s, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
