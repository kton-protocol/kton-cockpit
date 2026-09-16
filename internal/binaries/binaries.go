// Package binaries is the cockpit's whole surface onto the kton kernels. It reimplements none of
// their logic — canonicalization, hashing, signing, ingest, verification, the query semantics are
// all theirs — it calls them, as linked Go libraries, with the cockpit's resolved config.
//
// The name is older than the arrangement: these used to be subprocess wrappers around
// `bin/plankton` and `bin/nekton`, and it is kept because what the package IS has not changed —
// one place where every kernel call lives, so the rest of the cockpit sees one surface rather than
// a scattering of registry opens.
package binaries

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	nclaim "kton.dev/nekton/claim"
	nregistry "kton.dev/nekton/registry"
	ntemplate "kton.dev/nekton/template"
	"kton.dev/plankton/core"
	ffoton "kton.dev/plankton/foton"
	pregistry "kton.dev/plankton/registry"
)

// Runner is the one place the kernels are called from. Both are LINKED — `kton.dev/plankton` and
// `kton.dev/nekton` are ordinary Go dependencies — so nothing here spawns a process or parses
// another program's output. The registries, templates and keys it opens come from the cockpit's
// resolved, verified config, never from ambient PLANKTON_DIR/NEKTON_DIR environment variables:
// which store is written to is a property of the configuration, not of the shell that started it.
type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

// --- plankton ---

type AuthorInput struct {
	Inputs  []string // repo-relative paths, e.g. "data/penguins.csv"
	Outputs []string
	Cmd     string
	Located []string // "path=url" pairs for every input+output, from gitops.LocatedFlags
	SignKey string   // absolute path to the plankton signing key

	// Environment/EnvRef pin which execution environment produced this foton. Both are COVERED:
	// they ride in the descriptor into the foton id, so two runs of the same command in different
	// pinned environments are different fotons, and a reproduction commits to re-executing in the
	// pinned one. Empty means unpinned — which is a weaker record, not an invalid one.
	Environment string // qualified env-spectrum id (sha256:...)
	EnvRef      string // exact execution environment, e.g. oci://image@sha256:...
}

// Author builds the foton descriptor, signs it, and ingests it. Returns the foton id.
//
// The descriptor is what a foton's IDENTITY is computed over, so every default here is load-bearing
// and matches what `plankton author` produces for the same arguments: protocol kind "script", the
// descriptor as {cmd, environment?, envRef?}, and no automatic file:// locator — a logical path is
// the repo-relative one the caller gave. TestAuthor_MatchesTheReferenceCLI holds that equivalence
// against the binary, because "the same id" is not something to take on reading.
func (r *Runner) Author(ctx context.Context, in AuthorInput) (fotonID string, err error) {
	located := map[string][]string{}
	for _, l := range in.Located {
		path, uri, ok := strings.Cut(l, "=")
		if !ok {
			return "", fmt.Errorf("located %q is not path=uri", l)
		}
		located[path] = append(located[path], uri)
	}

	hashFiles := func(paths []string) ([]ffoton.FileSpec, error) {
		fs := make([]ffoton.FileSpec, 0, len(paths))
		for _, p := range paths {
			b, rerr := os.ReadFile(filepath.Join(r.cfg.RepoRoot, p))
			if rerr != nil {
				return nil, rerr
			}
			fs = append(fs, ffoton.FileSpec{Path: p, Hash: core.HashBytes(b), URI: located[p]})
		}
		return fs, nil
	}
	inputs, err := hashFiles(in.Inputs)
	if err != nil {
		return "", err
	}
	outputs, err := hashFiles(in.Outputs)
	if err != nil {
		return "", err
	}

	desc := map[string]any{"cmd": in.Cmd}
	if in.Environment != "" {
		desc["environment"] = in.Environment
	}
	if in.EnvRef != "" {
		desc["envRef"] = in.EnvRef
	}
	spec := ffoton.Spec{
		Predicate: "foton", Inputs: inputs, Outputs: outputs,
		Protocol: &ffoton.ProtocolSpec{Kind: "script", Descriptor: desc},
	}

	priv, err := loadSigningKey(in.SignKey)
	if err != nil {
		return "", err
	}
	env, id, err := ffoton.SignWith(spec, priv)
	if err != nil {
		return "", fmt.Errorf("signing the foton: %w", err)
	}
	reg, err := pregistry.Open(r.cfg.PlanktonDir)
	if err != nil {
		return "", fmt.Errorf("opening the plankton registry: %w", err)
	}
	if _, _, err := reg.Add(env); err != nil {
		return "", fmt.Errorf("the foton was signed but the registry refused it: %w", err)
	}
	return id, nil
}

// KeyID returns the keyid a signature carries for this public key, as the substrate derives it —
// `core.KeyIDHex`, not a local reimplementation of it. An identifier the kernel also derives is the
// kernel's to derive, or the two drift and every trust decision drifts with them.
func (r *Runner) KeyID(ctx context.Context, pubkeyPath string) (string, error) {
	b, err := os.ReadFile(pubkeyPath)
	if err != nil {
		return "", err
	}
	pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b)))
	if err != nil {
		return "", fmt.Errorf("%s is not a public key: %w", pubkeyPath, err)
	}
	return core.KeyIDHex(pub), nil
}

// Records returns every record this repo's plankton and nekton registries hold, in that order.
//
// It replaces reading them over `kton serve`'s /sync, which #83 removed with the observation that
// the cockpit was launching a server on a local port to talk to itself — HTTP as a worse CLI. #85
// answered the same kton §12 query over stdout; linking made even that a function call, so this is
// two registry opens where it used to be two servers, two free ports and a readiness poll.
//
// It still does not read the store. Which is the point: kton-web's own reader measures what parsing
// it wrongly costs — against a 2032-record corpus, a whole-file JSON.parse alone found 68 records
// and skipped 44 files "without a word", and the viewer drew a convincing lineage-only picture from
// it. Nothing errored.
func (r *Runner) Records(ctx context.Context) ([]Record, error) {
	preg, err := pregistry.Open(r.cfg.PlanktonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the plankton registry: %w", err)
	}
	nreg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}

	var all []Record
	for _, rec := range preg.Records(0) {
		env, merr := json.Marshal(rec.Envelope)
		if merr != nil {
			return nil, merr
		}
		all = append(all, Record{Seq: rec.Seq, FotonID: rec.FotonID, Envelope: env})
	}
	for _, rec := range nreg.Records(0) {
		env, merr := json.Marshal(rec.Envelope)
		if merr != nil {
			return nil, merr
		}
		all = append(all, Record{Seq: rec.Seq, ClaimID: rec.ClaimID, Envelope: env})
	}
	// A degraded read is INCOMPLETE and must never pass for a whole one. The CLI printed this on
	// stderr and the wrappers forwarded it; the registry reports it as a number, so it is checked
	// rather than forwarded as text somebody might not read.
	if n := preg.Degraded(); n > 0 {
		return nil, fmt.Errorf("the plankton registry skipped %d record(s) on load — this read is "+
			"INCOMPLETE, and a partial answer must not be returned as a whole one", n)
	}
	return all, nil
}

// EnvelopeFor returns one record's signed envelope, by id.
//
// Both registries are opened fresh, never cached on the Runner. A Runner outlives a write —
// publish authors and then reads, say annotates and then reads — so a cached registry would answer
// from before the write that just happened. Opening reads the whole store, which is exactly what
// each CLI invocation did too; the saving is the process, not the read.
func (r *Runner) EnvelopeFor(ctx context.Context, recordID string) (json.RawMessage, error) {
	if preg, err := pregistry.Open(r.cfg.PlanktonDir); err == nil {
		if env, ok := preg.Envelope(recordID); ok {
			return json.Marshal(env)
		}
	}
	nreg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	if rec, ok := nreg.Claim(recordID); ok {
		return json.Marshal(rec.Envelope)
	}
	return nil, fmt.Errorf("no record %s in this repo's registries", recordID)
}

// Hash returns the content hash of a file as `plankton hash` prints it.
func (r *Runner) Hash(ctx context.Context, file string) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.cfg.RepoRoot, file))
	if err != nil {
		return "", err
	}
	return core.HashBytes(b), nil
}

// Producer, Uses, Lineage, Reproductions are read-only graph queries by hash, answered from the
// registry's own index rather than from parsed text — a record's id comes from the record. They
// share plankton's degraded-read path, which can find fewer records than the store holds; that is
// reported rather than dropped, because an incomplete read presented as a complete answer is worse
// than an error.
func (r *Runner) Producer(ctx context.Context, hash string) (*LineageResult, error) {
	return r.lineage(ctx, "producer", hash)
}

func (r *Runner) Uses(ctx context.Context, hash string) (*LineageResult, error) {
	return r.lineage(ctx, "uses", hash)
}

func (r *Runner) Lineage(ctx context.Context, hash string) (*LineageResult, error) {
	return r.lineage(ctx, "lineage", hash)
}

func (r *Runner) lineage(ctx context.Context, relation, hash string) (*LineageResult, error) {
	reg, err := pregistry.Open(r.cfg.PlanktonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the plankton registry: %w", err)
	}
	if n, ok := core.NormalizeContentHash(hash); ok {
		hash = n
	}

	var ids []string
	switch relation {
	case "producer":
		ids = reg.Producer(hash)
	case "uses":
		ids = reg.Uses(hash)
	case "lineage":
		ids = reg.Lineage(hash)
	default:
		return nil, fmt.Errorf("unknown lineage relation %q", relation)
	}

	// In the registry's own order, which the CLI's --json form can no longer convey: it answers
	// records as bare envelopes with the ids beside them in a `summary` OBJECT, and a JSON object
	// has no order. For `producer` that costs nothing — the answer is a set — but `lineage` is a
	// walk toward the roots, and the sequence is the answer.
	res := &LineageResult{Relation: relation, Query: hash}
	for _, id := range ids {
		f, ok := reg.Foton(id)
		if !ok {
			continue
		}
		res.Records = append(res.Records, FotonRecord{
			ID: id, Kind: f.Protocol.Kind, Inputs: len(f.Inputs), Outputs: len(f.Outputs),
		})
	}
	if n := reg.Degraded(); n > 0 {
		return nil, fmt.Errorf("the plankton registry skipped %d record(s) on load — this lineage read "+
			"is INCOMPLETE, and a partial provenance answer must not be returned as a whole one", n)
	}
	return res, nil
}

// Reproductions returns plankton's ↻N report for outputHash, verified against this repo's
// configured trust tiers via --trust-keys — never plankton's bare, self-declared signer count.
// Without --trust-keys the count is forgeable (a relabeled keyid inflates ↻N and mis-attributes a
// reproduction to a party who never signed); the cockpit already holds exactly the keys that
// should count; it must pass them.
//
// tier scopes which keys are passed. It is not cosmetic: plankton computes the count itself over
// the keys it is given, so asking for one tier while handing it every tier's keys returns a number
// that silently spans all of them — an answer labelled as filtered that is not.
//
// Zero distinct producers is an answer, not a failure. The CLI signalled it with a non-zero exit
// and the answer printed anyway, which had to be told apart from a real refusal by whether stdout
// was empty; the library returns the count, so the distinction is structural and there is nothing
// left to misread.
func (r *Runner) Reproductions(ctx context.Context, outputHash, tier string) (*ReproductionsResult, error) {
	keys, err := TrustKeys(r.cfg, tier)
	if err != nil {
		return nil, err
	}
	// An empty key set would make the kernel fall back to the keyid each envelope claims about
	// ITSELF, which its author wrote — the forgeable count this path exists to refuse. It reports
	// that as Verified=false, but a wrong number under a field named for verification is read as a
	// number, so it is refused here rather than relabelled.
	if len(keys) == 0 {
		return nil, fmt.Errorf(
			"↻N would be self-declared: this repo's trust tiers name no key%s that could verify "+
				"anything, so the count would be over keyids the records assert about themselves, "+
				"which their authors wrote. Configure trust.tiers", tierNote(tier))
	}

	reg, err := pregistry.Open(r.cfg.PlanktonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the plankton registry: %w", err)
	}
	if n, ok := core.NormalizeContentHash(outputHash); ok {
		outputHash = n
	}
	got := reg.Reproductions(outputHash, keys)
	if !got.Verified {
		return nil, fmt.Errorf("the substrate answered a self-declared count for %s, not a verified "+
			"one; refusing to report it as ↻N", outputHash)
	}

	res := &ReproductionsResult{
		Output: got.Output, DistinctSigners: got.DistinctSigners,
		ProducerFotons: got.ProducerFotons, ExcludedUntrusted: got.ExcludedUntrusted,
		Trust: "verified",
	}
	for _, p := range got.Producers {
		res.Producers = append(res.Producers, ReproductionProducer{ID: p.FotonID, KeyID: p.KeyID, Verified: p.Verified})
	}
	return res, nil
}

func tierNote(tier string) string {
	if tier == "" {
		return ""
	}
	return " in tier " + strconv.Quote(tier)
}

// TrustKeys loads every configured public key, optionally narrowed to one tier.
//
// A key that will not read or parse is an ERROR, never a skip: skipping one silently narrows what
// the repository trusts, and every record it signed then reads as untrusted — a broken
// configuration wearing the appearance of a working one (SPEC §9.1).
func TrustKeys(cfg *config.Config, tier string) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for path, t := range cfg.TierPubkeys() {
		if tier != "" && t != tier {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("trust tier %q names %s, which cannot be read: %w", t, path, err)
		}
		pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("trust tier %q names %s, which is not a public key: %w", t, path, err)
		}
		keys = append(keys, pub)
	}
	return keys, nil
}

// trustKeysDir materializes every pubkey configured across this repo's trust tiers into a fresh
// flat directory of *.pub files — the shape `--trust-keys <dir>` expects (raw hex Ed25519, one key
// per *.pub file, loaded non-recursively). Callers must invoke the returned cleanup once done.
func trustKeysDir(cfg *config.Config, tier string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "cockpit-trust-keys-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(dir) }

	i := 0
	for pubkeyPath, keyTier := range cfg.TierPubkeys() {
		if tier != "" && keyTier != tier {
			continue
		}
		b, rerr := os.ReadFile(pubkeyPath)
		if rerr != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("reading trusted pubkey %s: %w", pubkeyPath, rerr)
		}
		dest := filepath.Join(dir, fmt.Sprintf("%d.pub", i))
		if werr := os.WriteFile(dest, b, 0o644); werr != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("writing trusted pubkey to %s: %w", dest, werr)
		}
		i++
	}
	return dir, cleanup, nil
}

// ReproducesResult is plankton's verdict on two sets of output bytes.
type ReproducesResult struct {
	// Level is "L0" (identical bytes) or "L1" (equal after the shared normalizer), and empty when
	// they do not reproduce at all. It is READ, never inferred from whether --via was passed:
	// plankton checks ref == cand BEFORE consulting a normalizer, so a byte-identical pair is L0
	// even when one was offered — and inferring "L1 whenever via != \"\"" mislabels a genuine L0 in
	// any repo that configures a default normalizer, which an L0 policy then rejects.
	Level   string `json:"level"`
	Matched bool   `json:"matched"`
	Via     string `json:"via"`
}

// Reproduces asks whether two output hashes reproduce, and at what level.
//
// The verdict and a refusal come back separately, which closes a gap this wrapper once documented
// as unclosable: `reproduces` exits 1 both for a genuine non-match and for a usage error, so a
// broken invocation would have been reported to the caller as "these outputs differ".
func (r *Runner) Reproduces(ctx context.Context, refHash, candHash, via string) (*ReproducesResult, error) {
	reg, err := pregistry.Open(r.cfg.PlanktonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the plankton registry: %w", err)
	}
	got, err := reg.Reproduces(refHash, candHash, via)
	if err != nil {
		// A refusal, not a verdict. The two used to arrive as one non-zero exit, and "they do not
		// match" reads the same as "those are not content hashes" when both are exit 1.
		return nil, fmt.Errorf("plankton could not compare %s with %s: %w", refHash, candHash, err)
	}
	return &ReproducesResult{Matched: got.Matches, Level: got.Level, Via: got.Via}, nil
}

func errOr(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

// --- nekton ---

// Annotate builds a claim from a template, signs it, ingests it, and returns its id. Callers
// follow up with About to confirm registration, mirroring the manual workflow's explicit confirm
// step — the id coming back says the claim was signed, not that the store kept it.
//
// The template is the ceiling: a claim's fields are whatever the template declares, so what a
// session can say is decided by files an operator put in `templates/`, not by this function.
func (r *Runner) Annotate(ctx context.Context, subject, tmplName string, sets map[string]string, signKey string, chain *Chain) (string, error) {
	set, err := r.templates()
	if err != nil {
		return "", err
	}
	t, ok := set.Get(tmplName)
	if !ok {
		return "", fmt.Errorf("no template %q in %s", tmplName, r.cfg.TemplatesDir)
	}

	// A `file` field carries BYTES, not a path: the package never touches a filesystem, so that it
	// links from a browser as readily as from here. Which fields those are comes from the template,
	// and reading them is this cockpit's job — with the same rule publish applies, since a claim's
	// evidence is committed and shared exactly as a published output is.
	values, files := map[string]string{}, map[string][]byte{}
	for k, v := range sets {
		if f, isField := t.Fields[k]; isField && f.Type == "file" {
			b, rerr := r.readRepoFile(v)
			if rerr != nil {
				return "", fmt.Errorf("field %q of template %q: %w", k, tmplName, rerr)
			}
			files[k] = b
			continue
		}
		values[k] = v
	}

	spec, err := set.Spec(tmplName, subject, values, files)
	if err != nil {
		return "", err
	}
	// Spec fills neither `by` nor `when`, and should not: the first is the caller's identity and
	// the second is the caller's clock, so a template package that supplied either would be
	// answering a question it cannot see. Both are covered by the claim id.
	//
	// So `set.Spec(...)` and `nclaim.SignWith(...)` do not compose on their own, and the seam fails
	// closed — SignWith refuses with "claim spec needs `by` and `when`" rather than signing a claim
	// that says nobody asserted it at no particular time. Filling them is this function's job, and
	// it is the only place that can do it.
	keyid, err := r.KeyID(ctx, pubHalfOf(signKey))
	if err != nil {
		return "", fmt.Errorf("reading the keyid to sign under: %w", err)
	}
	spec.By = "key:" + keyid
	spec.When = time.Now().UTC().Format(time.RFC3339)
	// Both or neither: the substrate refuses a scoped claim that carries no prev, so setting one
	// without the other would produce a signed claim the store then declines.
	if chain != nil {
		spec.Scope, spec.Prev = chain.Scope, chain.Prev
	}

	priv, err := loadSigningKey(signKey)
	if err != nil {
		return "", err
	}
	env, id, err := nclaim.SignWith(spec, priv)
	if err != nil {
		return "", fmt.Errorf("signing the claim: %w", err)
	}
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return "", fmt.Errorf("opening the nekton registry: %w", err)
	}
	if _, _, err := reg.Add(env); err != nil {
		return "", fmt.Errorf("the claim was signed but the registry refused it: %w", err)
	}
	return id, nil
}

// templates loads this repo's template set. An alias file is optional and this cockpit ships none:
// its templates name predicates by full IRI, so there is no prefix to resolve (SPEC §8.1).
func (r *Runner) templates() (ntemplate.Set, error) {
	set, err := ntemplate.Load(r.cfg.TemplatesDir, filepath.Join(r.cfg.RepoRoot, "aliases.json"))
	if err != nil {
		return ntemplate.Set{}, err
	}
	// Reported rather than absorbed: a file in the template directory that is not a template is
	// usually a typo'd one, and silence there means a claim shape the operator believes is
	// configured simply is not.
	if skipped := set.Skipped(); len(skipped) > 0 {
		return set, fmt.Errorf("%s holds %d file(s) that are not templates: %s",
			r.cfg.TemplatesDir, len(skipped), strings.Join(skipped, ", "))
	}
	return set, nil
}

// readRepoFile reads a repo-relative path, refusing anything that leaves the repository or names a
// signing key — the same floor publish holds, for the same reason: this content is committed and
// travels with the record.
func (r *Runner) readRepoFile(rel string) ([]byte, error) {
	if rel == "" {
		return nil, fmt.Errorf("no path given")
	}
	if filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("%q must be a repo-relative path inside the repository", rel)
	}
	if strings.HasSuffix(rel, ".key") {
		return nil, fmt.Errorf("%q matches *.key — a private signing key is never attached to a record", rel)
	}
	return os.ReadFile(filepath.Join(r.cfg.RepoRoot, rel))
}

// loadSigningKey reads a keygen-written private key: the 32-byte seed, as hex.
func loadSigningKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("%s is not a hex-encoded key: %w", path, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: expected a %d-byte seed, got %d", path, ed25519.SeedSize, len(seed))
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// About lists claims about a subject (hash or URI) — used both to serve `ask` and to confirm a
// `say` registered. It returns the claims themselves, so the object — what was actually said — is
// reachable; nekton's prose form carried only the id, predicate and declared signer (upstream #39),
// and a claim you cannot read the content of is a claim you cannot serve.
func (r *Runner) About(ctx context.Context, subject string) ([]ClaimAxis, error) {
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	return claimsFrom(reg.About(subject))
}

// ByAxis is which index `nekton by` searches. It is a required argument of that command, not an
// optional refinement: there is no "search everything" form.
type ByAxis string

const (
	BySigner    ByAxis = "signer"
	ByPredicate ByAxis = "predicate"
	ByObject    ByAxis = "object"
)

// ValidByAxis reports whether s names one of nekton's three `by` indexes.
func ValidByAxis(s string) bool {
	switch ByAxis(s) {
	case BySigner, ByPredicate, ByObject:
		return true
	}
	return false
}

// By lists claims indexed under one of the three axes. The axis argument is not optional: prior
// to this the cockpit called `nekton by <value>` with the axis omitted, which every version of
// nekton back to 0.1 rejects outright with its usage line — so `ask` with query "by" could never
// have returned an answer, despite being advertised in the tool's own schema.
func (r *Runner) By(ctx context.Context, axis ByAxis, value string) ([]ClaimAxis, error) {
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	switch axis {
	case BySigner:
		return claimsFrom(reg.BySigner(value))
	case ByPredicate:
		return claimsFrom(reg.ByPredicate(value))
	case ByObject:
		return claimsFrom(reg.ByObject(value))
	default:
		return nil, fmt.Errorf("unknown axis %q (signer, predicate, object)", axis)
	}
}

// claimsFrom decodes registry records into the axis shape the answer is built from.
//
// It marshals and re-parses rather than reaching into the statement directly, because
// parseClaimsJSON is where the payload's shape is understood — including the cases that matter:
// a payload that will not decode is REPORTED as an unreadable claim rather than dropped, and the
// signature keyids are kept as declared so nothing downstream mistakes them for verified ones.
// Two decoders would disagree exactly there.
func claimsFrom(recs []nregistry.Record) ([]ClaimAxis, error) {
	blob, err := json.Marshal(recs)
	if err != nil {
		return nil, err
	}
	return parseClaimsJSON(string(blob))
}

// --- verification material (kton §8.1) ---

// StoredMaterial is one piece of evidence as the kernel hands it back.
//
// The kernel carries a `verified` field on stored material that is ALWAYS false, and deliberately
// so: it stores material without evaluating it. That field is not mapped here, because carrying it
// would invite a caller to read a kernel-side verdict where none exists. Whether any of this checks
// out is decided by internal/material, on this side.
type StoredMaterial struct {
	Subject   string `json:"subject"`
	Scheme    string `json:"scheme"`
	MediaType string `json:"mediaType"`
	Material  string `json:"material"` // base64 of the scheme's own artifact, exactly as stored
}

// AttachFoton records evidence about a foton; AttachClaim does the same for a nekton claim.
//
// mediaType is always passed, never left to the kernel's default table — see config.Attachment.
func (r *Runner) AttachFoton(ctx context.Context, fotonID, scheme, mediaType, file string) error {
	return r.attach(ctx, "plankton", fotonID, scheme, mediaType, file)
}

func (r *Runner) AttachClaim(ctx context.Context, claimID, scheme, mediaType, file string) error {
	return r.attach(ctx, "nekton", claimID, scheme, mediaType, file)
}

func (r *Runner) attach(ctx context.Context, bin, id, scheme, mediaType, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	blob := base64.StdEncoding.EncodeToString(b)
	if bin == "plankton" {
		reg, oerr := pregistry.Open(r.cfg.PlanktonDir)
		if oerr != nil {
			return oerr
		}
		return reg.AttachMaterial(pregistry.VerificationMaterial{
			Subject: id, Scheme: scheme, MediaType: mediaType, Material: blob,
		})
	}
	reg, oerr := nregistry.Open(r.cfg.NektonDir)
	if oerr != nil {
		return oerr
	}
	return reg.AttachMaterial(nregistry.VerificationMaterial{
		Subject: id, Scheme: scheme, MediaType: mediaType, Material: blob,
	})
}

// MaterialForFoton reads back everything attached to a foton; MaterialForClaim does the same for a
// claim. An empty result is a normal answer, not an error: most records carry no material.
func (r *Runner) MaterialForFoton(ctx context.Context, fotonID string) ([]StoredMaterial, error) {
	return r.material(ctx, "plankton", fotonID)
}

func (r *Runner) MaterialForClaim(ctx context.Context, claimID string) ([]StoredMaterial, error) {
	return r.material(ctx, "nekton", claimID)
}

func (r *Runner) material(ctx context.Context, bin, id string) ([]StoredMaterial, error) {
	var out []StoredMaterial
	if bin == "plankton" {
		reg, err := pregistry.Open(r.cfg.PlanktonDir)
		if err != nil {
			return nil, err
		}
		for _, m := range reg.Material(id) {
			out = append(out, StoredMaterial{Subject: m.Subject, Scheme: m.Scheme, MediaType: m.MediaType, Material: m.Material})
		}
		return out, nil
	}
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, err
	}
	for _, m := range reg.Material(id) {
		out = append(out, StoredMaterial{Subject: m.Subject, Scheme: m.Scheme, MediaType: m.MediaType, Material: m.Material})
	}
	return out, nil
}

// --- scopes (kton §7.4) ---

// Chain places a claim in a scope's hash chain: the scope it belongs to, and the claim it follows.
// Both are required together — the substrate refuses a scoped claim with no prev.
type Chain struct {
	Scope string
	Prev  string
}

// ScopeHead is what `nekton head` reports about a scope's chain. Every field is load-bearing and
// none of them is prose: the kernel deliberately puts its two caveats in the data rather than in a
// line a reader may skip.
type ScopeHead struct {
	Scope string   `json:"scope"`
	Heads []string `json:"heads"`
	// ChainLength counts the claims chained under the seed. Zero is normal: the seed is its own tip.
	ChainLength int `json:"chainLength"`
	// Branched means claims share a prev, so each head commits only to its own branch. The kernel
	// reports the structure and prescribes no remedy — deciding what a seal over a branched scope
	// covers is a consumer's call (kton §7.4), which makes it this cockpit's.
	Branched bool `json:"branched"`
	// Unresolved counts claims that name this scope but whose prev is not held here. A withheld
	// MIDDLE claim leaves its successors unreachable, so the reported tip is PROVISIONAL rather
	// than final — the real head may be behind a claim this store has not seen.
	Unresolved int `json:"unresolved"`
}

// Head reads the current tip of a scope's chain.
//
// The tip comes from the substrate on every call and is never remembered here. A cockpit that
// cached it would be keeping mutable state about the chain (kton §13 forbids that), and would
// chain onto a stale tip the moment a mirror brought in a peer's claim.
func (r *Runner) Head(ctx context.Context, scopeID string) (*ScopeHead, error) {
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	heads, chainLen, ok := reg.Heads(scopeID)
	if !ok {
		return nil, fmt.Errorf("no such scope %s (not a seed ingested in %s)", scopeID, r.cfg.NektonDir)
	}
	if len(heads) == 0 {
		return nil, fmt.Errorf("the registry reported scope %s with no head at all; refusing to guess one", scopeID)
	}
	return &ScopeHead{
		Scope: scopeID, Heads: heads, ChainLength: chainLen,
		Branched: len(heads) > 1, Unresolved: reg.Unresolved(scopeID),
	}, nil
}

// ScopeChain returns every claim this registry holds that names scopeID, in the order the registry
// received them.
//
// Reading order from the registry rather than by walking prev is deliberate: a scope's claims live
// in one append-only file, so arrival order is a fact the store already has, while the prev links
// are the separate, independently checkable statement about what each writer believed preceded
// them. Keeping the two apart is what makes a disagreement between them visible at all.
func (r *Runner) ScopeChain(ctx context.Context, scopeID string) ([]ScopeClaim, error) {
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	recs := reg.Records(0)
	all, err := claimsFrom(recs)
	if err != nil {
		return nil, err
	}
	envelopes := map[string]json.RawMessage{}
	for _, rec := range recs {
		b, merr := json.Marshal(rec.Envelope)
		if merr != nil {
			return nil, merr
		}
		envelopes[rec.ClaimID] = b
	}
	chain := make([]ScopeClaim, 0, len(all))
	for _, c := range all {
		if c.Scope == scopeID {
			chain = append(chain, ScopeClaim{ClaimAxis: c, Envelope: envelopes[c.ID]})
		}
	}
	return chain, nil
}

// ScopeClaim is one claim in a scope, carried WITH its signed envelope.
//
// The envelope travels because a claim whose predecessor this registry cannot resolve is held but
// is not retrievable by id: `records` returns it and `verify <id>` answers "no claim in the
// registry". Verifying such a claim therefore has to go through its bytes. Reading that distinction
// out of the kernel's error text would be parsing prose, and treating any verify failure as
// "unresolvable" would swallow the operational errors SPEC §9.1 insists stay loud.
type ScopeClaim struct {
	ClaimAxis
	Envelope json.RawMessage
}

// Seed reports the scope's genesis statement — who opened it, when, under what name, and the
// `responsible` identities kton §7.4 lets a seed name. Membership is FIXED BY THE SEED, so this is
// where "who was supposed to write here" is written down; the kernel stores it and interprets none
// of it, and the reference implementation emits no `responsible` at all.
func (r *Runner) Seed(ctx context.Context, scopeID string) (*ScopeSeed, error) {
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return nil, fmt.Errorf("opening the nekton registry: %w", err)
	}
	rec, ok := reg.Claim(scopeID)
	if !ok {
		return nil, fmt.Errorf("no such scope %s (not a seed ingested in %s)", scopeID, r.cfg.NektonDir)
	}
	st, _, perr := nclaim.ParseEnvelope(rec.Envelope)
	if perr != nil {
		return nil, fmt.Errorf("the seed of scope %s is not readable: %w", scopeID, perr)
	}
	var body struct {
		Scope       string                `json:"scope"`
		Genesis     bool                  `json:"genesis"`
		By          string                `json:"by"`
		When        string                `json:"when"`
		Parent      struct{ Hash string } `json:"parent"`
		Responsible []string              `json:"responsible"`
	}
	if jerr := json.Unmarshal(st.Predicate, &body); jerr != nil {
		return nil, fmt.Errorf("the seed of scope %s is not readable: %w", scopeID, jerr)
	}
	if !body.Genesis {
		return nil, fmt.Errorf("%s is not a scope seed: its statement does not carry genesis", scopeID)
	}
	return &ScopeSeed{
		ID: scopeID, Name: body.Scope, By: body.By, When: body.When,
		Parent: body.Parent.Hash, Responsible: body.Responsible,
	}, nil
}

// ScopeSeed is a scope's genesis statement: the one place its membership is defined.
type ScopeSeed struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	By          string   `json:"by,omitempty"`
	When        string   `json:"when,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Responsible []string `json:"responsible,omitempty"`
}

// SealPredicate is the term a seal claim uses: the child scope is the subject, the head it was
// sealed at is the object.
//
// Minted rather than reused because nothing published states "this chain stood at this head". The
// nearest published candidates are about other things — schema:reviewedBy is a real property scoped
// to a WebPage, pav:hasCurrentVersion is about versions of a resource — and neither is a statement
// about a hash chain's tip. It lives in kton's namespace by its owner's grant and belongs in that
// project's vocabulary annex, so this cockpit is not squatting a namespace it does not own.
//
// Named `sealedAt` rather than `seal` because a predicate is a PROPERTY and has to read as one. A
// noun in the predicate slot is syntactically valid RDF that says nothing — the smaller sibling of
// putting an individual there, which is what `oa:assessing` as a predicate would have been. Form
// counts as much as existence: a term can be real, spelled right, and still be the wrong part of
// speech for the slot it is in.
const SealPredicate = "https://kton.dev/v/sealedAt"

// SealScope records a child scope's current head as a claim chained into its parent.
//
// Written through `nekton claim` rather than through a template, deliberately: a template is
// reachable from the claim ceiling a session writes against, and a seal that a session could forge
// would be worth nothing. This path takes no template and is only reachable from an operator
// subcommand.
//
// Sealing is repeatable and is meant to be repeated. Each seal fixes a point the chain can no
// longer be rewound behind, because the parent now carries that head: dropping the tail afterwards
// produces a chain whose head no longer matches what the parent recorded. That is what closes the
// gap SPEC §9.6 otherwise has to state as a limit — tail truncation is undetectable IN-BAND, and a seal
// is the out-of-band record that detects it.
func (r *Runner) SealScope(ctx context.Context, childScope, childHead, parentScope, parentHead, signKey string) (string, error) {
	keyid, err := r.KeyID(ctx, pubHalfOf(signKey))
	if err != nil {
		return "", fmt.Errorf("reading the keyid to seal under: %w", err)
	}
	// Built as a spec rather than through a template, deliberately: a template is what the claim
	// ceiling is expressed in, and a seal a session could forge would be worth nothing.
	spec := nclaim.Spec{
		Subject:   []nclaim.SubjectSpec{{Hash: childScope}},
		Predicate: SealPredicate,
		Object:    map[string]any{"hash": childHead},
		By:        "key:" + keyid,
		When:      time.Now().UTC().Format(time.RFC3339),
		Scope:     parentScope,
		Prev:      parentHead,
	}
	priv, err := loadSigningKey(signKey)
	if err != nil {
		return "", err
	}
	env, id, err := nclaim.SignWith(spec, priv)
	if err != nil {
		return "", fmt.Errorf("signing the seal: %w", err)
	}
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return "", fmt.Errorf("opening the nekton registry: %w", err)
	}
	if _, _, err := reg.Add(env); err != nil {
		return "", fmt.Errorf("the seal was signed but the registry refused it: %w", err)
	}
	return id, nil
}

// SeedScope opens a scope and returns its id. An operator action, never a verb: a scope exists
// before the session that writes into it.
func (r *Runner) SeedScope(ctx context.Context, name, signKey, parentScope string) (string, error) {
	keyid, err := r.KeyID(ctx, pubHalfOf(signKey))
	if err != nil {
		return "", fmt.Errorf("reading the keyid to seed under: %w", err)
	}
	body := map[string]any{
		"scope":   name,
		"genesis": true,
		"by":      "key:" + keyid,
		"when":    time.Now().UTC().Format(time.RFC3339),
	}
	if parentScope != "" {
		// A Ref — {hash} — not the subject shape {"digest":{"sha256":…}}. The two look
		// interchangeable and are not: a parent emitted in the wrong one resolves to nothing, in a
		// claim id that is permanent.
		h, ok := core.NormalizeContentHash(parentScope)
		if !ok {
			return "", fmt.Errorf("parent %q is not a scope id (sha256:<64 hex>)", parentScope)
		}
		body["parent"] = map[string]any{"hash": h}
	}
	// PredicateBody rather than the convenience fields: a seed has no predicate at all — it carries
	// scope/genesis/by/when/parent, which is a different statement shape (kton §7.4).
	spec := nclaim.Spec{
		Subject:       []nclaim.SubjectSpec{{URI: "urn:nekton:scope:" + name}},
		PredicateType: nclaim.ScopePredicateType,
		PredicateBody: body,
	}
	priv, err := loadSigningKey(signKey)
	if err != nil {
		return "", err
	}
	env, id, err := nclaim.SignWith(spec, priv)
	if err != nil {
		return "", fmt.Errorf("signing the seed: %w", err)
	}
	reg, err := nregistry.Open(r.cfg.NektonDir)
	if err != nil {
		return "", fmt.Errorf("opening the nekton registry: %w", err)
	}
	if _, _, err := reg.Add(env); err != nil {
		return "", fmt.Errorf("the seed was signed but the registry refused it: %w", err)
	}
	return id, nil
}

// pubHalfOf turns keys/x.key into keys/x.pub — keygen writes both side by side.
func pubHalfOf(privateKeyPath string) string {
	return strings.TrimSuffix(privateKeyPath, ".key") + ".pub"
}

// trimKeySuffix turns keys/x.key into keys/x.pub — the config names the private half, and the
// public half sits beside it under the same stem.
func trimKeySuffix(privateKeyPath string) string {
	return strings.TrimSuffix(privateKeyPath, ".key") + ".pub"
}
