package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/verify"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AskInput is cockpit_ask's argument shape — the "fragen" verb: query the graph. Filter can only
// narrow within the trust tiers configured for this repo; it can never surface a tier or signer
// absent from cockpit.config.json.
type AskInput struct {
	Query string `json:"query" jsonschema:"one of: producer, uses, lineage, reproductions, about, by"`
	Ref   string `json:"ref" jsonschema:"the hash, subject, or value to query"`
	// Axis is required for query "by" and ignored otherwise: nekton indexes claims under three
	// separate axes and has no combined search, so there is no defensible default to pick here.
	Axis   string     `json:"axis,omitempty" jsonschema:"for query \"by\" only: which index to search — signer, predicate, or object"`
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
	// Raw is the underlying plankton/nekton CLI output, kept for human/debugging context — but
	// redacted before being returned: every record NOT in Included (excluded for any reason —
	// unverified, or verified but outside a requested trustTier filter) has its line replaced with
	// a placeholder. Every query this tool supports prints exactly one record per line, each line
	// carrying that record's own id (see redactExcluded), so this is a safe, generic way to keep
	// unverified content from ever reaching the model — not just advisory metadata layered around
	// unfiltered content.
	Raw string `json:"raw"`
	// omitempty matters here, not just for tidiness: without it, jsonschema-go marks these fields
	// required, and a nil slice (the zero value returned on any error path) marshals to JSON null —
	// which then fails the SDK's own output-schema validation with a confusing "type: null, want
	// array" error that masks whatever the real error was.
	Records  []RecordVerification `json:"records,omitempty"`
	Included []string             `json:"included,omitempty"`
	Excluded []string             `json:"excluded,omitempty"`
	// Claims carries the decoded claim axis for the "about" and "by" queries — what each claim
	// actually says, not merely that it exists. It holds ONLY included claims: unlike Raw, which is
	// scraped text that has to be redacted line by line, this is built from parsed records, so an
	// unverified or filtered-out claim is never assembled into it in the first place.
	Claims []binaries.ClaimAxis `json:"claims,omitempty"`
	// Fotons is the lineage-query counterpart of Claims: the foton records "producer", "uses",
	// "lineage" and "reproductions" found, again only the included ones.
	Fotons []binaries.FotonRecord `json:"fotons,omitempty"`
	// VerifiedSigners is plankton's own ↻N for a "reproductions" query — distinct signers whose
	// signature actually verified against the trust keys the cockpit passed it, never a
	// self-declared count. Scoped to the requested tier when the query asked for one.
	VerifiedSigners int    `json:"verifiedSigners,omitempty"`
	FilterApplied   string `json:"filterApplied"`
}

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
	wantTier := ""
	if in.Filter != nil {
		wantTier = in.Filter.TrustTier
	}

	// The two families answer in different shapes and are handled separately rather than being
	// forced through one path. nekton's about/by have a --json mode, so their claims are parsed and
	// filtered structurally. plankton's lineage queries have no --json mode at all, so those stay
	// on scraped text with the line-anchored redaction below.
	switch in.Query {
	case "about", "by":
		return askClaims(ctx, cfg, r, in, wantTier)
	case "producer", "uses", "lineage", "reproductions":
		return askLineage(ctx, cfg, r, in, wantTier)
	default:
		return errResult[AskOutput]("unknown query %q (must be one of: producer, uses, lineage, reproductions, about, by)", in.Query)
	}
}

// askClaims serves the nekton queries. Every claim is verified by id, and only claims that verify
// into a configured trust tier (and, if a filter was given, into that tier) are assembled into the
// answer at all — the excluded ones are accounted for in Records/Excluded but their content is
// never rendered anywhere.
func askClaims(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, wantTier string) (*mcp.CallToolResult, AskOutput, error) {
	var claims []binaries.ClaimAxis
	var err error

	switch in.Query {
	case "about":
		claims, err = r.About(ctx, in.Ref)
	case "by":
		if in.Axis == "" {
			return errResult[AskOutput]("query \"by\" requires axis (signer, predicate, or object) — nekton indexes claims under three separate axes and has no combined search")
		}
		if !binaries.ValidByAxis(in.Axis) {
			return errResult[AskOutput]("unknown axis %q for query \"by\" (must be one of: signer, predicate, object)", in.Axis)
		}
		claims, err = r.By(ctx, binaries.ByAxis(in.Axis), in.Ref)
	}
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	records := make([]RecordVerification, 0, len(claims))
	included := []string{}
	excluded := []string{}
	includedClaims := []binaries.ClaimAxis{}
	var lines []string

	for _, c := range claims {
		tier, _, verr := verify.ResolveTier(ctx, r, cfg, c.ID, verify.Claim)
		if verr != nil {
			return errResult[AskOutput]("verifying %s failed: %v", c.ID, verr)
		}
		verified := tier != verify.Untrusted
		records = append(records, RecordVerification{ID: c.ID, Tier: tier, Verified: verified})

		if !verified || (wantTier != "" && tier != wantTier) {
			excluded = append(excluded, c.ID)
			continue
		}
		included = append(included, c.ID)
		includedClaims = append(includedClaims, c)
		lines = append(lines, c.Line())
	}

	return &mcp.CallToolResult{}, AskOutput{
		Query:         in.Query,
		Ref:           in.Ref,
		Raw:           strings.Join(lines, "\n"),
		Records:       records,
		Included:      included,
		Excluded:      excluded,
		Claims:        includedClaims,
		FilterApplied: filterDescription(wantTier),
	}, nil
}

// askLineage serves the plankton queries. Every foton the query names is verified by id, and only
// fotons that verify into a configured trust tier (and, if a filter was given, into that tier) are
// assembled into the answer — the rest are accounted for in Records/Excluded, never rendered.
//
// This used to scrape ids out of plankton's prose with a regex and then blank whole lines of it.
// That worked only while a record's own id happened to be the first hash on its line — an
// assumption no doc guaranteed and no test upstream protected. kton #57 gave these four queries a
// --json mode where the id is a named field, so the assumption is gone rather than defended.
func askLineage(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, wantTier string) (*mcp.CallToolResult, AskOutput, error) {
	if in.Query == "reproductions" {
		return askReproductions(ctx, cfg, r, in, wantTier)
	}

	var res *binaries.LineageResult
	var err error
	switch in.Query {
	case "producer":
		res, err = r.Producer(ctx, in.Ref)
	case "uses":
		res, err = r.Uses(ctx, in.Ref)
	case "lineage":
		res, err = r.Lineage(ctx, in.Ref)
	}
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filterDescription(wantTier)}
	var lines []string
	for _, rec := range res.Records {
		included, verr := siftFoton(ctx, cfg, r, rec.ID, wantTier, &out)
		if verr != "" {
			return errResult[AskOutput]("%s", verr)
		}
		if included {
			out.Fotons = append(out.Fotons, rec)
			lines = append(lines, rec.Line())
		}
	}
	out.Raw = joinWithWarning(lines, res.Warning)
	return &mcp.CallToolResult{}, out, nil
}

// askReproductions answers ↻N. The count is plankton's own, computed over exactly the keys the
// cockpit hands it — which is why the tier filter is passed down rather than applied afterwards: a
// count taken over every tier and then labelled as one tier's would be a filtered answer that is
// not filtered.
func askReproductions(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, wantTier string) (*mcp.CallToolResult, AskOutput, error) {
	res, err := r.Reproductions(ctx, in.Ref, wantTier)
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filterDescription(wantTier)}
	var lines []string
	for _, p := range res.Producers {
		included, verr := siftFoton(ctx, cfg, r, p.ID, wantTier, &out)
		if verr != "" {
			return errResult[AskOutput]("%s", verr)
		}
		if included {
			out.Fotons = append(out.Fotons, binaries.FotonRecord{ID: p.ID})
			lines = append(lines, fmt.Sprintf("%s  by key:%s", p.ID, p.KeyID))
		}
	}
	out.VerifiedSigners = res.DistinctSigners
	summary := fmt.Sprintf("reproductions: %d distinct verified signer(s) produced %s (%d producer foton(s))",
		res.DistinctSigners, res.Output, res.ProducerFotons)
	if res.ExcludedUntrusted > 0 {
		summary += fmt.Sprintf("; %d producer foton(s) excluded — signed by no key this repo trusts",
			res.ExcludedUntrusted)
	}
	out.Raw = joinWithWarning(append([]string{summary}, lines...), res.Warning)
	return &mcp.CallToolResult{}, out, nil
}

// siftFoton verifies one foton id and records the verdict on out, reporting whether it belongs in
// the answer. The second return value is a non-empty error message.
func siftFoton(ctx context.Context, cfg *config.Config, r *binaries.Runner, id, wantTier string, out *AskOutput) (bool, string) {
	tier, _, err := verify.ResolveTier(ctx, r, cfg, id, verify.Foton)
	if err != nil {
		return false, fmt.Sprintf("verifying %s failed: %v", id, err)
	}
	verified := tier != verify.Untrusted
	out.Records = append(out.Records, RecordVerification{ID: id, Tier: tier, Verified: verified})
	if !verified || (wantTier != "" && tier != wantTier) {
		out.Excluded = append(out.Excluded, id)
		return false, ""
	}
	out.Included = append(out.Included, id)
	return true, ""
}

// joinWithWarning appends plankton's success-path stderr, if any, as a clearly delimited block —
// a degraded "this read is INCOMPLETE" notice must not vanish just because it was not an error.
func joinWithWarning(lines []string, warning string) string {
	raw := strings.Join(lines, "\n")
	if warning != "" {
		raw += "\n\n[stderr]\n" + warning
	}
	return raw
}

func filterDescription(wantTier string) string {
	if wantTier != "" {
		return fmt.Sprintf("trustTier=%s", wantTier)
	}
	return "none — every record verified against a configured trust tier is included; unverified records are always excluded"
}
