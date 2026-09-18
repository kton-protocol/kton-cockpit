package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/verify"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AskInput is cockpit_ask's argument shape — the "fragen" verb: query the graph. Filter can only
// narrow within the trust tiers configured for this repo; it can never surface a tier or signer
// absent from cockpit.config.json.
type AskInput struct {
	Query string `json:"query" jsonschema:"one of: producer, uses, lineage, reproductions, about, by, scope"`
	Ref   string `json:"ref" jsonschema:"the hash, subject, or value to query"`
	// Axis is required for query "by" and ignored otherwise: nekton indexes claims under three
	// separate axes and has no combined search, so there is no defensible default to pick here.
	Axis   string     `json:"axis,omitempty" jsonschema:"for query \"by\" only: which index to search — signer, predicate, or object"`
	Filter *AskFilter `json:"filter,omitempty"`
}

// AskFilter narrows an answer. Every dimension can only ever REMOVE records: none of them can
// surface a record the trust configuration would otherwise have excluded, and none can reach past
// the configured tiers. A filter is a question about the answer, not a way to widen it.
//
// A value naming something that does not exist is refused rather than matching nothing. An unknown
// tier, level or signer would otherwise empty the answer and read exactly like "this repository
// trusts none of this" — a typo presenting as a finding.
type AskFilter struct {
	// TrustTier restricts to records a key in that configured tier verified.
	TrustTier string `json:"trustTier,omitempty" jsonschema:"only include records verified against this configured trust tier"`
	// Signer restricts to records a specific key verified — the keyid of a configured public key,
	// never the keyid a record declares about itself.
	Signer string `json:"signer,omitempty" jsonschema:"only include records verified by this keyid (a configured key's, not a record's declared one)"`
	// Level restricts claims to a reproduction level: L0, L1 or L2.
	Level string `json:"level,omitempty" jsonschema:"only include reproduces claims at this level: L0, L1 or L2"`
	// Scope restricts claims to one nekton scope.
	Scope string `json:"scope,omitempty" jsonschema:"only include claims chained under this scope id"`
	// MinReproductions drops a reproductions answer that does not reach this many verified
	// independent producers. The count is the verified one; a self-declared ↻N is refused outright.
	MinReproductions int `json:"minReproductions,omitempty" jsonschema:"for query reproductions: require at least this many verified independent producers"`
}

// active reports whether this filter says anything at all.
//
// A NON-ZERO threshold counts, not a positive one: a negative value narrows nothing but is still
// something the caller wrote, and treating it as an absent filter would skip the validation that
// tells them it is meaningless.
func (f *AskFilter) active() bool {
	return f != nil && (f.TrustTier != "" || f.Signer != "" || f.Level != "" || f.Scope != "" || f.MinReproductions != 0)
}

// describe says what was applied, because requirement 2 of the governing brief is that the active
// filter travels with the answer: "no trustworthy cleanup" and "no cleanup" are different findings,
// and a reader who cannot see which filter ran cannot tell them apart.
func (f *AskFilter) describe() string {
	if !f.active() {
		return "none — every record verified against a configured trust tier is included; unverified records are always excluded"
	}
	var parts []string
	if f.TrustTier != "" {
		parts = append(parts, "trustTier="+f.TrustTier)
	}
	if f.Signer != "" {
		parts = append(parts, "signer="+f.Signer)
	}
	if f.Level != "" {
		parts = append(parts, "level="+f.Level)
	}
	if f.Scope != "" {
		parts = append(parts, "scope="+f.Scope)
	}
	if f.MinReproductions > 0 {
		parts = append(parts, fmt.Sprintf("minReproductions=%d", f.MinReproductions))
	}
	return strings.Join(parts, ", ")
}

// RecordVerification is what the record actually verified as — resolved from the verifying key,
// never from the record's own declared keyid/by field.
type RecordVerification struct {
	ID       string `json:"id"`
	Tier     string `json:"tier"`
	Verified bool   `json:"verified"`
	// Material is the external evidence attached to this record (kton §8.1), each item carrying
	// whether this cockpit evaluated it. Present only for INCLUDED records, for the same reason Raw
	// is redacted: evidence attached to a record that did not verify is content from an untrusted
	// record, and surfacing it would route around the filter it was excluded by.
	//
	// It is a read per record, so it is gathered only where it can be reported. Empty means nothing
	// is attached — which is the normal case and says nothing bad about the record.
	Material []material.Report `json:"material,omitempty"`
}

type AskOutput struct {
	Query string `json:"query"`
	Ref   string `json:"ref"`
	// Raw is human/debugging context, ASSEMBLED FROM INCLUDED RECORDS ONLY. A record excluded for
	// any reason — unverified, or verified but outside a requested filter — never contributes a line
	// to it.
	//
	// It used to work the other way around: the kernel's text output was scraped and the lines of
	// excluded records were blanked afterwards. That held only under an assumption nothing
	// guaranteed — that a record's id is the first hash on its line — and plankton's own usage text
	// now says the opposite. kton #57 made the reads structured, so an excluded record is not
	// redacted out of the answer, it is never built into it. See internal/binaries/reads.go.
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
	VerifiedSigners int `json:"verifiedSigners,omitempty"`
	// Seal is the verdict for query "scope": whether the chain reaches its seed without a gap over
	// the sources this repo holds, who defined the scope, and who has written into it.
	Seal *SealVerdict `json:"seal,omitempty"`
	// Record is the answer to query "record": what one run actually did — its command, the
	// environment it ran in, and every file it named going in and coming out.
	Record *binaries.FotonDetail `json:"record,omitempty"`
	// SignerName is what this repo's trust configuration calls the key that verified, when it
	// names it at all. A keyid is not a person; the tiers are the only place that says whose is whose.
	SignerName    string `json:"signerName,omitempty"`
	FilterApplied string `json:"filterApplied"`
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
	if msg := validateFilter(ctx, cfg, r, in.Filter); msg != "" {
		return errResult[AskOutput]("%s", msg)
	}
	filter := in.Filter
	if filter == nil {
		filter = &AskFilter{}
	}

	// The two families answer in different shapes and are handled separately rather than being
	// forced through one path. nekton's about/by have a --json mode, so their claims are parsed and
	// filtered structurally. plankton's lineage queries have no --json mode at all, so those stay
	// on scraped text with the line-anchored redaction below.
	switch in.Query {
	case "about", "by":
		return askClaims(ctx, cfg, r, in, filter)
	case "producer", "uses", "lineage", "reproductions":
		return askLineage(ctx, cfg, r, in, filter)
	case "record":
		return askRecord(ctx, cfg, r, in, filter)
	case "scope":
		// The one query that asks about a STRUCTURE rather than about a record, and the only place
		// this cockpit reaches a verdict about completeness — which kton §7.4 assigns to a consumer
		// and evaluates "when the seal is relied upon". Relying on it is a read.
		out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filter.describe()}
		verdict, msg := askScope(ctx, cfg, r, in.Ref, &out)
		if msg != "" {
			return errResult[AskOutput]("%s", msg)
		}
		out.Seal = verdict
		out.Raw = verdict.Line()
		return &mcp.CallToolResult{}, out, nil
	default:
		return errResult[AskOutput]("unknown query %q (must be one of: record, producer, uses, lineage, reproductions, about, by, scope)", in.Query)
	}
}

// askClaims serves the nekton queries. Every claim is verified by id, and only claims that verify
// into a configured trust tier (and, if a filter was given, into that tier) are assembled into the
// answer at all — the excluded ones are accounted for in Records/Excluded but their content is
// never rendered anywhere.
func askClaims(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, filter *AskFilter) (*mcp.CallToolResult, AskOutput, error) {
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
		tier, verifyingKey, verr := verify.ResolveTier(ctx, r, cfg, c.ID, verify.Claim)
		if verr != nil {
			return errResult[AskOutput]("verifying %s failed: %v", c.ID, verr)
		}
		verified := tier != verify.Untrusted
		records = append(records, RecordVerification{ID: c.ID, Tier: tier, Verified: verified})

		if !verified || !filter.keeps(tier, c) {
			excluded = append(excluded, c.ID)
			continue
		}
		mats, merr := material.Describe(ctx, cfg, r, c.ID, material.Claim, verifyingKey)
		if merr != nil {
			return errResult[AskOutput]("reading the verification material on %s failed: %v", c.ID, merr)
		}
		records[len(records)-1].Material = mats
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
		FilterApplied: filter.describe(),
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
func askLineage(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, filter *AskFilter) (*mcp.CallToolResult, AskOutput, error) {
	if in.Query == "reproductions" {
		return askReproductions(ctx, cfg, r, in, filter)
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

	keyidOf, kerr := keyidsByPath(ctx, cfg, r)
	if kerr != nil {
		return errResult[AskOutput]("resolving this repo's configured keys failed: %v", kerr)
	}

	out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filter.describe()}
	var lines []string
	for _, rec := range res.Records {
		included, verr := siftFoton(ctx, cfg, r, rec.ID, filter, keyidOf, &out)
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
func askReproductions(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, filter *AskFilter) (*mcp.CallToolResult, AskOutput, error) {
	res, err := r.Reproductions(ctx, in.Ref, filter.TrustTier)
	if err != nil {
		return errResult[AskOutput]("query failed: %v", err)
	}

	keyidOf, kerr := keyidsByPath(ctx, cfg, r)
	if kerr != nil {
		return errResult[AskOutput]("resolving this repo's configured keys failed: %v", kerr)
	}

	out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filter.describe()}
	var lines []string
	for _, p := range res.Producers {
		included, verr := siftFoton(ctx, cfg, r, p.ID, filter, keyidOf, &out)
		if verr != "" {
			return errResult[AskOutput]("%s", verr)
		}
		if included {
			out.Fotons = append(out.Fotons, binaries.FotonRecord{ID: p.ID})
			lines = append(lines, fmt.Sprintf("%s  by key:%s", p.ID, p.KeyID))
		}
	}
	out.VerifiedSigners = res.DistinctSigners
	// A threshold on the count is the one dimension that can empty an answer without excluding any
	// single record: the records are fine, there are just not enough of them. Said plainly rather
	// than by returning nothing.
	if filter.MinReproductions > 0 && res.DistinctSigners < filter.MinReproductions {
		out.Included, out.Fotons, lines = nil, nil, nil
		summaryPrefix := fmt.Sprintf(
			"below the requested threshold: %d verified independent producer(s), %d asked for.\n",
			res.DistinctSigners, filter.MinReproductions)
		out.Raw = summaryPrefix
		out.FilterApplied = filter.describe()
		return &mcp.CallToolResult{}, out, nil
	}
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
func siftFoton(ctx context.Context, cfg *config.Config, r *binaries.Runner, id string, filter *AskFilter, keyidOf map[string]string, out *AskOutput) (bool, string) {
	tier, verifyingKey, err := verify.ResolveTier(ctx, r, cfg, id, verify.Foton)
	if err != nil {
		return false, fmt.Sprintf("verifying %s failed: %v", id, err)
	}
	verified := tier != verify.Untrusted
	out.Records = append(out.Records, RecordVerification{ID: id, Tier: tier, Verified: verified})
	// A foton carries no level and no scope; those dimensions are claim-shaped, so a lineage answer
	// is narrowed by tier and signer only. Naming a claim dimension on a lineage query is not an
	// error — it simply has nothing to act on here, and describe() still reports it, so a reader can
	// see that the filter they asked for did not apply rather than assuming it did.
	if !verified || !filter.keepsFoton(tier, keyidOf[verifyingKey]) {
		out.Excluded = append(out.Excluded, id)
		return false, ""
	}
	mats, merr := material.Describe(ctx, cfg, r, id, material.Foton, verifyingKey)
	if merr != nil {
		return false, fmt.Sprintf("reading the verification material on %s failed: %v", id, merr)
	}
	out.Records[len(out.Records)-1].Material = mats
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

// validateFilter refuses a filter naming something this repo does not have. The second return value
// is a non-empty error message.
func validateFilter(ctx context.Context, cfg *config.Config, r *binaries.Runner, f *AskFilter) string {
	if !f.active() {
		return ""
	}
	if f.TrustTier != "" {
		if _, ok := cfg.Raw.Trust.Tiers[f.TrustTier]; !ok {
			return fmt.Sprintf(
				"no trust tier named %q is configured (this repo has: %s) — an unknown tier would match "+
					"nothing and read as though nothing here verified",
				f.TrustTier, strings.Join(sortedTierNames(cfg), ", "))
		}
	}
	switch f.Level {
	case "", "L0", "L1", "L2":
	default:
		return fmt.Sprintf("level %q is not one of L0, L1, L2", f.Level)
	}
	if f.MinReproductions < 0 {
		return "minReproductions cannot be negative"
	}
	if f.Signer != "" {
		// The keyid must name a key this repo CONFIGURES. Accepting any keyid would be filtering on
		// what a record says about itself, which is the thing SPEC §9.1 refuses to use.
		byPath, err := keyidsByPath(ctx, cfg, r)
		if err != nil {
			return fmt.Sprintf("could not resolve this repo's configured keys: %v", err)
		}
		known := make([]string, 0, len(byPath))
		for _, kid := range byPath {
			if kid == f.Signer {
				return ""
			}
			known = append(known, kid)
		}
		sort.Strings(known)
		return fmt.Sprintf(
			"no configured key has keyid %q (this repo configures: %s) — filtering on a keyid this repo "+
				"does not hold would be filtering on what a record claims about itself",
			f.Signer, strings.Join(known, ", "))
	}
	return ""
}

// sortedTierNames lists this repo's configured tiers, for an error that says what IS available.
func sortedTierNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Raw.Trust.Tiers))
	for n := range cfg.Raw.Trust.Tiers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// keepsFoton decides whether a FOTON survives. A foton carries no level and no scope, so only the
// tier and the verifying key apply; the claim-shaped dimensions have nothing here to act on, and
// describe() still reports them so a reader sees that what they asked for did not bite.
func (f *AskFilter) keepsFoton(tier, verifiedBy string) bool {
	if f.TrustTier != "" && tier != f.TrustTier {
		return false
	}
	if f.Signer != "" && verifiedBy != f.Signer {
		return false
	}
	return true
}

// keyidsByPath maps each configured pubkey path to the keyid a signature carries for it. Resolved
// once per call rather than per record: it is one substrate invocation per configured key, and the
// answer cannot change while a call is running.
func keyidsByPath(ctx context.Context, cfg *config.Config, r *binaries.Runner) (map[string]string, error) {
	out := map[string]string{}
	for path := range cfg.TierPubkeys() {
		kid, err := r.KeyID(ctx, path)
		if err != nil {
			return nil, err
		}
		out[path] = kid
	}
	return out, nil
}

// keeps decides whether a CLAIM survives the filter, given the tier it verified into.
func (f *AskFilter) keeps(tier string, c binaries.ClaimAxis) bool {
	if f.TrustTier != "" && tier != f.TrustTier {
		return false
	}
	if f.Scope != "" && c.Scope != f.Scope {
		return false
	}
	if f.Level != "" && claimLevel(c) != f.Level {
		return false
	}
	if f.Signer != "" && !carriesSignature(c, f.Signer) {
		return false
	}
	return true
}

// claimLevel reads the reproduction level out of a claim's object, or "" when it carries none. The
// object is free-form vocabulary, so this reads a field rather than assuming a shape.
func claimLevel(c binaries.ClaimAxis) string {
	obj, ok := c.Object.(map[string]any)
	if !ok {
		return ""
	}
	lvl, _ := obj["level"].(string)
	return lvl
}

// carriesSignature reports whether keyid signed this claim. It reads the envelope's signature list,
// which is what the record actually carries — the filter is only reached for a record that already
// verified (SPEC §9.1), so this narrows within what verification established rather than replacing it.
func carriesSignature(c binaries.ClaimAxis, keyid string) bool {
	for _, k := range c.SignatureKeyIDs {
		if k == keyid {
			return true
		}
	}
	return false
}

// askRecord answers "what did this run actually do", given the id a publish returned.
//
// Every other lineage query takes a FILE hash, which makes a foton id an answer and never a
// question: the thing the cockpit hands you when you publish could not be used to ask it anything.
// Two first-time users hit that independently and both named it their biggest obstacle, and both
// worked around it by decoding the registry's stored payloads themselves.
//
// The signer is reported as the key that actually verified, and named when this repo's
// configuration names it — a keyid is not a person, and the trust tiers are the only place that
// says whose key is whose.
func askRecord(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskInput, filter *AskFilter) (*mcp.CallToolResult, AskOutput, error) {
	out := AskOutput{Query: in.Query, Ref: in.Ref, FilterApplied: filter.describe()}
	rec, err := r.FotonByID(ctx, in.Ref)
	if err != nil {
		if !strings.HasPrefix(in.Ref, "sha256:") {
			return errResult[AskOutput](
				"%v\n\nThis query takes the foton id a publish returned (sha256:…), not a path. "+
					"To go the other way — from bytes to the run that made them — use producer.", err)
		}
		return errResult[AskOutput]("%v", err)
	}

	tier, _, verr := verify.ResolveTier(ctx, r, cfg, rec.ID, verify.Foton)
	if verr != nil {
		return errResult[AskOutput]("verifying %s: %v", rec.ID, verr)
	}
	verified := tier != verify.Untrusted
	out.Records = []RecordVerification{{ID: rec.ID, Tier: string(tier), Verified: verified}}
	if verified {
		out.Included = []string{rec.ID}
	} else {
		out.Excluded = []string{rec.ID}
	}
	out.Record = rec
	out.SignerName = signerName(cfg, rec.SignerKeyID)
	out.Raw = recordLines(rec, out.SignerName)
	return &mcp.CallToolResult{}, out, nil
}

// signerName turns a keyid into whatever this repo's configuration calls that key, or "" when it
// names no such key. Derived from the trust tiers because that is the only place in the system
// that connects a key to a name at all.
func signerName(cfg *config.Config, keyID string) string {
	if keyID == "" {
		return ""
	}
	for tier, paths := range cfg.Raw.Trust.Tiers {
		for _, p := range paths {
			b, err := os.ReadFile(filepath.Join(cfg.RepoRoot, p))
			if err != nil {
				continue
			}
			pub, perr := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
			if perr != nil {
				continue
			}
			if core.KeyIDHex(pub) == keyID {
				name := strings.TrimSuffix(filepath.Base(p), ".pub")
				return name + " (" + tier + ")"
			}
		}
	}
	return ""
}

func recordLines(rec *binaries.FotonDetail, signer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", rec.ID)
	fmt.Fprintf(&b, "  cmd      %s\n", rec.Cmd)
	if rec.EnvRef != "" {
		fmt.Fprintf(&b, "  ran in   %s\n", rec.EnvRef)
	}
	who := rec.SignerKeyID
	if signer != "" {
		who = signer + "  key:" + rec.SignerKeyID
	}
	fmt.Fprintf(&b, "  signed   %s\n", who)
	for _, f := range rec.Inputs {
		fmt.Fprintf(&b, "  in       %-52s %s\n", f.Path, short(f.Hash))
	}
	for _, f := range rec.Outputs {
		fmt.Fprintf(&b, "  out      %-52s %s\n", f.Path, short(f.Hash))
	}
	return b.String()
}

func short(h string) string {
	if len(h) > 23 {
		return h[:23] + "\u2026"
	}
	return h
}
