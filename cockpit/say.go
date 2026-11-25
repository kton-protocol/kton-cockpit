package cockpit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/anchor"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"github.com/kton-protocol/kton-cockpit/internal/show"
)

// SayRequest is cockpit_say's argument shape — the "sagen" verb: bind a claim, from an allowed
// template, to a foton or file. The caller names a template and supplies field values; for the
// "reproduces" template specifically, the cockpit — not the caller — determines the resulting
// level/reproducedBy fields by actually running the reproduction precondition.
type SayRequest struct {
	Subject  string            `json:"subject" jsonschema:"the sha256 foton id or hash the claim is about"`
	Template string            `json:"template" jsonschema:"the claim template name (must be in this repo's allowed-templates config)"`
	Fields   map[string]string `json:"fields,omitempty" jsonschema:"template field values, for templates other than reproduces"`

	// reproduces-only fields: the cockpit computes level/reproducedBy itself from these.
	SubjectOutputHash string `json:"subjectOutputHash,omitempty" jsonschema:"output hash of the producer foton being reproduced (reproduces template only)"`
	ReproducedOutput  string `json:"reproducedOutput,omitempty" jsonschema:"repo-relative path of the caller's own output file that reproduces the subject (reproduces template only)"`
	ReproducedFotonID string `json:"reproducedFotonId,omitempty" jsonschema:"foton id of the caller's own producer foton for reproducedOutput (reproduces template only)"`
	Via               string `json:"via,omitempty" jsonschema:"optional shared normalizer ref, for an L1 (not L0) reproduction; omitted, the normaliser of the reproduced run's environment class, then the repo's reproduction.normalizer"`

	// Scope names one of the scopes this repo configures, chaining the claim into that
	// conversation. Omitted, the claim stands on its own — which is the common case. The name must
	// be one of claims.scopes: the operator fixes the set, exactly as with the template ceiling.
	Scope string `json:"scope,omitempty" jsonschema:"optional: the name of a configured claim scope to chain this claim into"`
	// ExpectHead is the head of Scope the caller last saw. When given, the claim is chained onto
	// exactly that head, and refused if it is no longer one: another process or an agent wrote
	// first, and appending anyway would fork the conversation without anyone deciding to.
	ExpectHead string `json:"expectHead,omitempty" jsonschema:"optional, with scope: the scope head (claim id) you last saw; refused if the head has moved since"`
	// ExpectTemplate and ExpectRevision bind the claim to the template the caller showed a person: the
	// sha256 of the template's bytes (a package template's member hash, as ui list gives it), and the
	// package revision it came from. Either refuses template.changed when the template say would sign
	// with is not that one, so a consent given in a dialog holds (kton-review R1 on #177).
	ExpectTemplate string `json:"expectTemplate,omitempty" jsonschema:"optional: the sha256 of the template's bytes you showed; refused (template.changed) if the template is another"`
	// ExpectPredicate is the predicate as the caller listed it (say --json): a change of the alias file
	// changes what is signed without changing the template's bytes (kton-review R1b on #177).
	ExpectPredicate string `json:"expectPredicate,omitempty" jsonschema:"optional: the predicate (full IRI) say --json listed for the template; refused (template.changed) if it now resolves to another"`
	ExpectRevision  string `json:"expectRevision,omitempty" jsonschema:"optional: the package revision the template came from when you showed it; refused (template.changed) if it comes from another"`
	// As names who the claim is said as: a key in identity.keyring whose public half a trust tier
	// holds. Omitted, the claim is signed with this repository's nekton_key.
	As string `json:"as,omitempty" jsonschema:"optional: sign as this key of identity.keyring (<name>-claims.key), which must be in a trust tier"`
}

type SayResult struct {
	ClaimID string `json:"claimId"`
	Level   string `json:"level,omitempty"`
	// Via is the normaliser potential a reproduces comparison was offered, and ViaFrom where it came
	// from: "call", "spectrum <id> in <package>", or "reproduction.normalizer". Set for reproduces
	// only, whether or not the match needed it (an L0 never does).
	Via          string `json:"via,omitempty"`
	ViaFrom      string `json:"viaFrom,omitempty"`
	Confirmation string `json:"confirmation"`
	// ResolvedFrom is set when the subject was a file in the repository: the claim is about its bytes
	// (Subject), and this is the path it was given as.
	ResolvedFrom string `json:"resolvedFrom,omitempty"`
	Subject      string `json:"subject,omitempty"`
	// Pending is set when the claim is signed and stored but its commit met the repository's lock
	// (store.busy): it exists, and the next write commits it. A caller must not say it again.
	Pending *Pending `json:"pending,omitempty"`
	// Replaces is the claim this one replaces when say filled it for a template with
	// "replace": "previous": this signer's newest own claim of that template about the subject.
	Replaces string `json:"replaces,omitempty"`
	// WaitingOn is set when this repository's own registry holds the claim deferred, waiting for a predecessor
	// that only a federation source holds (the scope's head or seed came from there): the scope it waits in.
	// The union of the registries resolves the claim; the own registry does by itself once it holds the
	// predecessor.
	WaitingOn string `json:"waitingOn,omitempty"`
	// RekorLogIndex and RekorUUID are set when this repo anchors its records — see PublishResult.
	RekorLogIndex int64  `json:"rekorLogIndex,omitempty"`
	RekorUUID     string `json:"rekorUuid,omitempty"`
	// Chain is where this claim joined its scope, present only when one is configured. It reports
	// what this registry could see at the time and judges none of it: whether the chain is whole is
	// a seal-verification question, answered by `cockpit_ask` with query "scope".
	Chain *ChainPosition `json:"chain,omitempty"`
	// Kept are the files the claim is about, its subject and its file fields, and where their bytes were
	// kept: shared (.kton/blobs, committed with the claim), local (.kton/local/blobs, this checkout only:
	// git ignores the file) or no, with why (B11).
	Kept []KeptFile `json:"kept,omitempty"`
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

// preparedSay is a claim checked and ready to sign: everything say decides before it reads where the
// claim goes. Say and SayBatch take the same path to it.
type preparedSay struct {
	in           SayRequest
	resolvedFrom string
	signKey      string
	expect       binaries.Expect
	sets         map[string]string
	via, viaFrom string
}

// prepareSay makes the checks say makes before it takes the claims lock: the template ceiling, the
// subject, the identity, the vocabulary, the expectations, the fields, and the reproduction
// precondition for reproduces.
func prepareSay(ctx context.Context, cfg *config.Config, r *binaries.Runner, in SayRequest) (*preparedSay, error) {
	if ok, verr := templateAllowed(cfg, in.Template); verr != nil {
		return nil, refuse("vocabulary", "SPEC §8.1", "%v", verr)
	} else if !ok {
		return nil, refuse("template.not-allowed", "SPEC §8.1", "template %q is not in this repo's allowed claim templates", in.Template)
	}
	if in.Subject == "" {
		return nil, refuse("argument", "", "say requires subject (the foton id or hash the claim is about)")
	}
	subject, resolvedFrom, why := resolveSubject(cfg, in.Subject)
	if why != "" {
		return nil, refuse("argument", "SPEC §8", "%s%s", why, fullIDHint(ctx, cfg, in.Subject))
	}
	// A value that starts as an id and is not a whole one is refused in every field, whatever its type: a
	// string field would otherwise sign the short form as text, a decision about nothing ask can find
	// (userinterface, dry run 1 finding 6b3b60e3).
	fields := make([]string, 0, len(in.Fields))
	for k := range in.Fields {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	for _, k := range fields {
		if v := strings.TrimSpace(in.Fields[k]); isPartialID(v) {
			return nil, refuse("argument", "SPEC §8", "field %s = %q is not a whole id (sha256: and 64 hex digits)%s", k, v, fullIDHint(ctx, cfg, v))
		}
	}
	in.Subject = subject
	p := &preparedSay{in: in, resolvedFrom: resolvedFrom}

	signKey := cfg.NektonKey
	if in.As != "" {
		k, why := keyAs(cfg, in.As)
		if why != "" {
			return nil, refuse("identity.as", "SPEC §8.7", "%s", why)
		}
		signKey = k
	}
	p.signKey = signKey

	if why := binaries.MisplacedAliases(cfg); why != "" {
		return nil, refuse("vocabulary", "SPEC §5.6", "%s", why)
	}
	if in.ExpectTemplate != "" {
		h, ok := core.NormalizeContentHash(in.ExpectTemplate)
		if !ok {
			return nil, refuse("argument", "SPEC §8", "expectTemplate is a content hash (sha256:<hex>), not %q", in.ExpectTemplate)
		}
		p.expect.Template = h
	}
	p.expect.Revision, p.expect.Predicate = in.ExpectRevision, in.ExpectPredicate
	sets := map[string]string{}
	for k, v := range in.Fields {
		sets[k] = v
	}
	if in.Template != "reproduces" {
		FillIdentity(cfg, in.Template, in.As, sets)
	}

	if in.Template == "reproduces" {
		via, viaFrom, verr := reproductionVia(ctx, cfg, r, in)
		if verr != nil {
			return nil, refuse("reproduction.normalizer", "SPEC §8.2", "%v", verr)
		}
		in.Via = via
		level, err := determineReproductionLevel(ctx, cfg, r, in)
		if err != "" {
			return nil, refuse("reproduction.not-met", "SPEC §8.2", "%s", err)
		}
		p.via, p.viaFrom = via, viaFrom
		sets = map[string]string{"level": level, "reproducedBy": in.ReproducedFotonID}
	}
	p.in.Via = in.Via
	p.sets = sets
	return p, nil
}

func (c *Cockpit) Say(ctx context.Context, in SayRequest) (*SayResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if !cfg.Raw.Verbs.Say {
		return nil, refuse("verb.disabled", "SPEC §6", "say is disabled by this repo's cockpit.config.json")
	}
	r := binaries.New(cfg)
	tr := newTrace("say")
	defer tr.end()
	p, prepErr := prepareSay(ctx, cfg, r, in)
	tr.mark("prepare (template, subject, fields)")
	if prepErr != nil {
		return nil, prepErr
	}
	in, sets, signKey, resolvedFrom := p.in, p.sets, p.signKey, p.resolvedFrom
	reproducedVia, reproducedViaFrom := p.via, p.viaFrom
	r.ExpectTemplate, r.ExpectRevision, r.ExpectPredicate = p.expect.Template, p.expect.Revision, p.expect.Predicate

	// From reading where the claim goes (its scope's head, the claim it replaces) until it is stored,
	// no other claim is said in this repository, so two at once never take the same head.
	unlockClaims, lerr := r.LockClaims()
	if lerr != nil {
		return nil, refuse("store", "", "%v", lerr)
	}
	claimsLocked := true
	releaseClaims := func() {
		if claimsLocked {
			claimsLocked = false
			unlockClaims()
		}
	}
	defer releaseClaims()
	tr.mark("the claims lock")
	chain, head, cerr := resolveChain(ctx, r, cfg, in.Scope)
	tr.mark("the scope's head (reads the union)")
	if cerr != "" {
		return nil, refuse("scope.unknown", "", "%s", cerr)
	}
	if in.ExpectHead != "" {
		want, ok := core.NormalizeContentHash(in.ExpectHead)
		switch {
		case chain == nil:
			return nil, refuse("argument", "SPEC §8.5", "expectHead needs scope: it is a head of the scope the claim joins")
		case !ok:
			return nil, refuse("argument", "SPEC §8.5", "expectHead is a claim id (sha256:<hex>), not %q", in.ExpectHead)
		case !slices.Contains(head.Heads, want):
			return nil, refuse("scope.moved", "SPEC §8.5", "the head of scope %s is no longer %s but %s: read what was written since, then say again",
				in.Scope, want, strings.Join(head.Heads, ", "))
		}
		chain.Prev = want
	}

	// replace: previous fills replaces with this signer's own previous claim (ADR-013 A1/A5, M2),
	// unless the request names one; the answer says which.
	var replacedPrev string
	if in.Template != "reproduces" && sets["replaces"] == "" {
		prev, perr := previousOwn(ctx, cfg, r, in.Template, in.Subject, signKey)
		if perr != nil {
			return nil, refuse("kernel", "", "finding the claim this one replaces: %v", perr)
		}
		if prev != "" {
			sets["replaces"], replacedPrev = prev, prev
		}
	}
	tr.mark("the previous claim")
	claimID, err := r.Annotate(ctx, in.Subject, in.Template, sets, signKey, chain)
	releaseClaims()
	tr.mark("sign and store")
	var changed *binaries.TemplateChanged
	if errors.As(err, &changed) {
		return nil, refuse("template.changed", "SPEC §8", "%s", changed.Reason)
	} else if err != nil {
		return nil, refuse("kernel", "", "nekton annotate failed: %v", err)
	}

	// A claim carries the same configured evidence a foton does. A cockpit whose certificate rode
	// only on its fotons would say who produced a result and leave who VOUCHED for it unattributed —
	// and a claim is exactly the record where that question is being asked.
	if err := material.Attach(ctx, cfg, r, claimID, material.Claim); err != nil {
		return nil, refuse("material", "SPEC §11", "%v", err)
	}

	var anchored *anchor.Entry
	if cfg.Raw.Anchor.Enabled {
		anchored, err = anchor.Record(ctx, cfg, claimID, anchor.Claim)
		if err != nil {
			return nil, refuse("anchor", "", "the claim was registered but anchoring it failed: %v", err)
		}
	}

	registryPaths := []string{config.Rel("nekton")}
	// The bytes of the files the claim is about are kept with it (B11): the subject's, compared with the
	// hash it was signed as, and the file fields'.
	var keepPaths []string
	keepWant := map[string]string{}
	if resolvedFrom != "" {
		keepPaths = append(keepPaths, resolvedFrom)
		keepWant[resolvedFrom] = in.Subject
	}
	if set, err := r.Templates(); err == nil {
		keepPaths = append(keepPaths, fileFieldPaths(set, in.Template, sets)...)
	}
	if beforeKeep != nil {
		beforeKeep()
	}
	kept, keptShared := keepSigned(ctx, cfg, keepPaths, keepWant, signedHashes(cfg, claimID))
	if keptShared {
		registryPaths = append(registryPaths, config.Rel("blobs"))
	}
	if cfg.Raw.Union.Publish {
		written, uerr := show.WriteUnion(ctx, cfg)
		if uerr != nil {
			return nil, refuse("union", "", "the claim was registered but publishing the union failed: %v", uerr)
		}
		registryPaths = append(registryPaths, written...)
	}
	_, perr := persistStrict(ctx, cfg, registryPaths, "claim: "+in.Template+" on "+in.Subject)
	tr.mark("persist (commit, push)")
	pending, err := pendingIfBusy(perr, "the claim "+claimID)
	if err != nil {
		return nil, refuse(storeCode(err), "", "git commit/push of the claim registry failed: %v", err)
	}

	// The claim went into this repository's own registry, so that is where it is confirmed: reading the
	// union fetched and joined every federation source for a check none of them can answer (T12).
	confirmation, waitingOn, err := confirmSay(ctx, r, claimID, in.Subject, tr)
	if err != nil {
		return nil, err
	}

	level := sets["level"]
	out := SayResult{
		ClaimID:      claimID,
		Level:        level,
		Confirmation: confirmation,
		WaitingOn:    waitingOn,
		Subject:      in.Subject,
		ResolvedFrom: resolvedFrom,
		Pending:      pending,
		Replaces:     replacedPrev,
		Kept:         kept,
	}
	out.Via, out.ViaFrom = reproducedVia, reproducedViaFrom
	if anchored != nil {
		out.RekorLogIndex, out.RekorUUID = anchored.LogIndex, anchored.UUID
	}
	if chain != nil {
		out.Chain = &ChainPosition{
			Scope: chain.Scope, Prev: chain.Prev,
			Length: head.ChainLength, Heads: head.Heads, Unresolved: head.Unresolved,
		}
	}
	return &out, nil
}

// confirmSay checks that a claim a say just signed registered, and says how. The manual workflow confirms a
// claim by querying it back rather than trusting the write; the cockpit does the same, so a claim that
// signed but never registered is not reported as success.
//
// The own registry answers first. A claim chained onto a predecessor that only a federation source holds (a
// teammate's claim as the scope's head, a seed from a source) is held there deferred and stays out of about,
// so only then is the union of the own registry and the sources read, in place: it resolves the claim, and
// the answer says what it waits for.
func confirmSay(ctx context.Context, r *binaries.Runner, claimID, subject string, tr *trace) (confirmation, waitingOn string, err error) {
	claims, err := r.OwnAbout(subject)
	tr.mark("confirm (reads the own registry)")
	if err != nil {
		return "", "", refuse("kernel", "", "claim was recorded but confirmation query (nekton about) failed: %v", err)
	}
	for _, c := range claims {
		if c.ID == claimID {
			return fmt.Sprintf("registered: nekton reports %s among the %d claim(s) this repository holds about %s", claimID, len(claims), subject), "", nil
		}
	}
	waiting, deferred, err := r.OwnDeferred(claimID)
	if err != nil {
		return "", "", refuse("kernel", "", "claim was recorded but this repository's registry cannot be read: %v", err)
	}
	if !deferred {
		return "", "", refuse("claim.not-registered", "",
			"claim %s was signed but is not among the %d claim(s) this repository's registry holds about %s, and the registry does not hold it deferred either — it did not register",
			claimID, len(claims), subject)
	}
	resolved, uerr := r.UnionResolves(ctx, claimID)
	tr.mark("confirm (reads the federation)")
	if uerr != nil || !resolved {
		why := "no registry of the federation holds it"
		if uerr != nil {
			why = fmt.Sprintf("the federation could not be read: %v", uerr)
		}
		return "", "", refuse("claim.not-registered", "",
			"claim %s was signed and is held in this repository's registry, but it waits for its predecessor in scope %s and %s — it is not registered",
			claimID, waiting, why)
	}
	return fmt.Sprintf("registered: %s is held in this repository's registry waiting for its predecessor in scope %s, which the federation holds, and the union of the registries resolves it", claimID, waiting), waiting, nil
}

// reproductionVia picks the normaliser a reproduces comparison is offered, in this order: the one
// the call names; the one that comes with the environment class of the run being reproduced, found
// in an installed package's spectrum (MH 11.11.: the normaliser comes with the environment, not
// with the project); the repo's reproduction.normalizer. The second value says which it was.
func reproductionVia(ctx context.Context, cfg *config.Config, r *binaries.Runner, in SayRequest) (string, string, error) {
	if in.Via != "" {
		return in.Via, "call", nil
	}
	if class := subjectEnvironment(ctx, r, in); class != "" {
		pot, from, err := spectrumNormalizer(cfg, class)
		if err != nil {
			return "", "", err
		}
		if pot != "" {
			return pot, "spectrum " + class + " in " + from, nil
		}
	}
	if n := cfg.Raw.Reproduction.Normalizer; n != "" {
		return n, "reproduction.normalizer", nil
	}
	return "", "", nil
}

// determineReproductionLevel runs the reproduction precondition itself (never trusting a
// self-declared level from the caller) and returns either the achieved level ("L0"/"L1") or, as
// its second return value, a non-empty error message.
func determineReproductionLevel(ctx context.Context, cfg *config.Config, r *binaries.Runner, in SayRequest) (level string, errMsg string) {
	if in.SubjectOutputHash == "" || in.ReproducedOutput == "" || in.ReproducedFotonID == "" {
		return "", "reproduces requires subjectOutputHash, reproducedOutput, and reproducedFotonId"
	}

	candHash, err := r.Hash(ctx, in.ReproducedOutput)
	if err != nil {
		return "", "plankton hash of reproducedOutput failed: " + err.Error()
	}

	via := in.Via // chosen by reproductionVia

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
		// A scope this repository opened for an installation (`cockpit install`) is named by its
		// seed and needs no configuration entry: install is the operator's act that opened it.
		if own, err := r.OwnScope(ctx, name, cfg.NektonKey); err != nil {
			return nil, nil, err.Error()
		} else if own != "" {
			scope, ok = own, true
		}
	}
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
