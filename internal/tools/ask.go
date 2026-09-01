package tools

import (
	"context"
	"fmt"
	"regexp"
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
	Claims        []binaries.ClaimAxis `json:"claims,omitempty"`
	FilterApplied string               `json:"filterApplied"`
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

// askLineage serves the plankton queries, whose output is text with one record per line.
func askLineage(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, wantTier string) (*mcp.CallToolResult, AskOutput, error) {
	var raw string
	var err error
	switch in.Query {
	case "producer":
		raw, err = r.Producer(ctx, in.Ref)
	case "uses":
		raw, err = r.Uses(ctx, in.Ref)
	case "lineage":
		raw, err = r.Lineage(ctx, in.Ref)
	case "reproductions":
		raw, err = r.Reproductions(ctx, in.Ref)
	}
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	// Exclude the query subject itself from the extracted ids: it's what was ASKED about, not a
	// record the query FOUND, but it's often echoed back verbatim in the raw text (e.g.
	// reproductions' own summary line, or producer/uses/lineage's "(none) - <ref> is a lineage
	// root..." message). Left in, it would get run through verification like any other record —
	// and since a query subject essentially never itself verifies as a record (an output hash
	// looked up as a foton id normally won't resolve), it would always land in Excluded and get
	// its own line redacted below, which for reproductions means redacting the line with the
	// actual answer.
	ids := excludeRef(uniqueMatches(recordIDRe, raw), in.Ref)
	records := make([]RecordVerification, 0, len(ids))
	included := []string{}
	excluded := []string{}
	excludedReasons := map[string]string{}

	for _, id := range ids {
		tier, _, verr := verify.ResolveTier(ctx, r, cfg, id, verify.Foton)
		if verr != nil {
			return errResult[AskOutput]("verifying %s failed: %v", id, verr)
		}
		verified := tier != verify.Untrusted
		records = append(records, RecordVerification{ID: id, Tier: tier, Verified: verified})

		if !verified {
			excluded = append(excluded, id)
			excludedReasons[id] = "not verified against any configured trust tier"
			continue
		}
		if wantTier != "" && tier != wantTier {
			excluded = append(excluded, id)
			excludedReasons[id] = fmt.Sprintf("verified as trust tier %q, but this query requested trustTier=%q", tier, wantTier)
			continue
		}
		included = append(included, id)
	}

	return &mcp.CallToolResult{}, AskOutput{
		Query:         in.Query,
		Ref:           in.Ref,
		Raw:           redactExcluded(raw, excludedReasons),
		Records:       records,
		Included:      included,
		Excluded:      excluded,
		FilterApplied: filterDescription(wantTier),
	}, nil
}

func filterDescription(wantTier string) string {
	if wantTier != "" {
		return fmt.Sprintf("trustTier=%s", wantTier)
	}
	return "none — every record verified against a configured trust tier is included; unverified records are always excluded"
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

// excludeRef drops ref from ids, case-insensitively — see the comment at its call site in Ask.
func excludeRef(ids []string, ref string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !strings.EqualFold(id, ref) {
			out = append(out, id)
		}
	}
	return out
}

// redactExcluded replaces every line of raw whose OWN record id is in excludedReasons with a
// placeholder, so unverified (or filtered-out) content never reaches whatever reads Raw. Every
// plankton/nekton query this tool calls prints one record per line, and — verified against the
// actual print statements for all six supported query types — that record's own id is always the
// FIRST sha256 hash on its line (plankton's producer/uses/lineage/reproductions listings; nekton's
// printClaims for about/by). Anchoring to the first match specifically (not "does this line
// contain an excluded id anywhere") matters: a line can legitimately contain a SECOND, unrelated
// sha256 hash that must never trigger redaction of an otherwise-trusted line — e.g. a nekton
// claim's own predicate is spec-allowed to itself be a content hash (a term reference), which
// almost never resolves as a real claim and so is almost always "excluded" in its own right, even
// though the claim carrying it is fully verified; naive substring matching would wipe the whole
// trusted claim's line because of that unrelated embedded hash. (Same risk, lower probability:
// plankton's free-text `kind=` field on producer/uses/lineage lines is not an enum and could in
// principle embed another hash.) The query subject itself is filtered out before ids ever reaches
// the verification loop — see excludeRef — specifically so a summary line that merely echoes it,
// e.g. reproductions', is never mistaken for an excluded record's line either.
func redactExcluded(raw string, excludedReasons map[string]string) string {
	if len(excludedReasons) == 0 {
		return raw
	}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		id := recordIDRe.FindString(line)
		if id == "" {
			continue
		}
		if reason, isExcluded := excludedReasons[id]; isExcluded {
			lines[i] = fmt.Sprintf("[excluded: %s — %s]", id, reason)
		}
	}
	return strings.Join(lines, "\n")
}
