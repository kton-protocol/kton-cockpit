// Package binaries shells out to the vendored plankton/nekton binaries. It never reimplements
// any of their logic (canonicalization, hashing, signing, registry, verification) — it only
// invokes them with the cockpit's resolved config and parses their plain-text stdout.
package binaries

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// Runner shells out to plankton/nekton with PLANKTON_DIR/NEKTON_DIR/NEKTON_TEMPLATES pinned from
// the cockpit's resolved, verified config — never inherited from ambient shell env vars.
type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

func (r *Runner) env() []string {
	return []string{
		"PLANKTON_DIR=" + r.cfg.PlanktonDir,
		"NEKTON_DIR=" + r.cfg.NektonDir,
		"NEKTON_TEMPLATES=" + r.cfg.TemplatesDir,
	}
}

// exec runs bin with args, capturing stdout and stderr into separate buffers — never combined:
// plankton/nekton's own contract (see e.g. `author --print-id`) is bare, machine-readable output
// on stdout and human/warning/error lines on stderr. Both are returned so each caller can decide
// what it needs: a machine value parsed from stdout alone, or stdout plus any success-path
// warning on stderr (e.g. plankton's "this read is INCOMPLETE" degraded-registry notice, which
// prints even on a clean exit — see planktonQuery below). On a non-zero exit, stderr is also
// folded into the returned error for debugging context.
func (r *Runner) exec(ctx context.Context, bin string, args ...string) (stdout string, stderr string, err error) {
	binPath := filepath.Join(r.cfg.BinDir, bin)
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = r.cfg.RepoRoot
	cmd.Env = append(cmd.Env, r.env()...)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	stdout = strings.TrimSpace(outBuf.String())
	stderr = strings.TrimSpace(errBuf.String())
	if runErr != nil {
		return stdout, stderr, fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), runErr, stderr)
	}
	return stdout, stderr, nil
}

// plankton returns plankton's bare stdout only, discarding stderr on success. Use this for
// commands whose result is (or is parsed as) an exact machine-readable value — Author's fotonID,
// Hash's hash, Reproduces'/VerifyFoton's pass/fail — where any success-path stderr text is status
// noise that must not contaminate the parsed value (Author's --print-id relies on this: with it
// set, plankton deliberately routes every human status line to stderr, leaving only the bare id
// on stdout).
func (r *Runner) plankton(ctx context.Context, args ...string) (string, error) {
	out, _, err := r.exec(ctx, "plankton", args...)
	return out, err
}

// planktonQuery is like plankton, but for human-facing reads whose stdout is a report rather than
// a single parsed value (producer/uses/lineage): plankton can legitimately warn on stderr even on
// a clean exit — e.g. "warning: N record(s) skipped on load - this read is INCOMPLETE" when the
// registry read is degraded but not `--strict`. That warning must still reach the caller (and, via
// AskOutput.Raw, the model) rather than silently vanish just because it wasn't a parse error, so
// it's appended to the returned text as a clearly delimited block instead of being discarded.
func (r *Runner) planktonQuery(ctx context.Context, args ...string) (string, error) {
	out, errText, err := r.exec(ctx, "plankton", args...)
	if errText != "" {
		out += "\n\n[stderr]\n" + errText
	}
	return out, err
}

func (r *Runner) nekton(ctx context.Context, args ...string) (string, error) {
	out, _, err := r.exec(ctx, "nekton", args...)
	return out, err
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

// Author runs `plankton author` and returns the printed foton id.
func (r *Runner) Author(ctx context.Context, in AuthorInput) (fotonID string, err error) {
	args := []string{"author"}
	for _, p := range in.Inputs {
		args = append(args, "--in", p)
	}
	for _, p := range in.Outputs {
		args = append(args, "--out", p)
	}
	for _, l := range in.Located {
		args = append(args, "--located", l)
	}
	if in.Environment != "" {
		args = append(args, "--environment", in.Environment)
	}
	if in.EnvRef != "" {
		args = append(args, "--env-ref", in.EnvRef)
	}
	args = append(args, "--cmd", in.Cmd, "--sign", in.SignKey, "--add", "--print-id")

	out, err := r.plankton(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// KeyID returns the keyid a signature carries for this public key, as the substrate derives it.
// Shelled out rather than computed here for the usual reason: an identifier the kernel also derives
// is the kernel's to derive.
func (r *Runner) KeyID(ctx context.Context, pubkeyPath string) (string, error) {
	out, err := r.plankton(ctx, "keyid", pubkeyPath)
	return strings.TrimSpace(out), err
}

// Records returns every record this repo's plankton and nekton registries hold, in that order.
//
// It replaces reading them over `kton serve`'s /sync, which #83 removed with the observation that
// the cockpit was launching a server on a local port to talk to itself — HTTP as a worse CLI. #85
// answered the same kton §12 query over stdout, so this is two subprocess calls where it used to be
// two servers, two free ports and a readiness poll.
//
// It still does not read the store. Which is the point: kton-web's own reader measures what parsing
// it wrongly costs — against a 2032-record corpus, a whole-file JSON.parse alone found 68 records
// and skipped 44 files "without a word", and the viewer drew a convincing lineage-only picture from
// it. Nothing errored.
func (r *Runner) Records(ctx context.Context) ([]Record, error) {
	var all []Record
	for _, bin := range []string{"plankton", "nekton"} {
		out, _, err := r.exec(ctx, bin, "records", "--json")
		if err != nil {
			return nil, fmt.Errorf("reading records from %s: %w", bin, err)
		}
		recs, perr := parseRecordsJSON(out)
		if perr != nil {
			return nil, fmt.Errorf("reading records from %s: %w", bin, perr)
		}
		all = append(all, recs...)
	}
	return all, nil
}

// EnvelopeFor returns one record's signed envelope, by id. Fotons come from `plankton show --json`
// and claims from `nekton about --json`; a caller that knows which it holds saves the miss.
func (r *Runner) EnvelopeFor(ctx context.Context, recordID string) (json.RawMessage, error) {
	recs, err := r.Records(ctx)
	if err != nil {
		return nil, err
	}
	for _, rec := range recs {
		if rec.ID() == recordID {
			return rec.Envelope, nil
		}
	}
	return nil, fmt.Errorf("no record %s in this repo's registries", recordID)
}

// Hash returns the content hash of a file as `plankton hash` prints it.
func (r *Runner) Hash(ctx context.Context, file string) (string, error) {
	return r.plankton(ctx, "hash", file)
}

// Show prints a foton's descriptor (command, inputs, outputs).
func (r *Runner) Show(ctx context.Context, idOrFile string) (string, error) {
	return r.plankton(ctx, "show", idOrFile)
}

// Producer, Uses, Lineage, Reproductions are read-only graph queries by hash, read through
// plankton's --json mode so a record's id comes from a named field rather than from guessing at
// text (see reads.go). Producer/Uses/Lineage share plankton's degraded-registry-read code path,
// which can warn "INCOMPLETE" on stderr even on a clean exit — that warning is carried through on
// the result rather than dropped, because an incomplete read presented as a complete answer is
// worse than an error.
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
	out, errText, err := r.exec(ctx, "plankton", relation, "--json", hash)
	if err != nil {
		return nil, err
	}
	res, perr := parseLineageJSON(out)
	if perr != nil {
		return nil, perr
	}
	res.Warning = errText
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
// The binary exits non-zero for the legitimate "0 distinct producers" case (not just for real
// failures), and prints that answer before exiting — so a non-zero exit with output is a valid,
// informational zero-count result. A non-zero exit with EMPTY stdout means the command rejected the
// call outright (most plausibly a vendored plankton predating --trust-keys, which kton 0.2 provides
// and 0.1 did not) rather than answering "zero producers", and is surfaced as a real error instead
// of being handed to Claude as if it were a count.
func (r *Runner) Reproductions(ctx context.Context, outputHash, tier string) (*ReproductionsResult, error) {
	dir, cleanup, err := trustKeysDir(r.cfg, tier)
	if err != nil {
		return nil, fmt.Errorf("materializing trust keys for reproductions: %w", err)
	}
	defer cleanup()

	out, errText, err := r.exec(ctx, "plankton", "reproductions", "--trust-keys", dir, "--json", outputHash)
	if err != nil && out == "" {
		return nil, fmt.Errorf(
			"plankton rejected `reproductions --trust-keys` outright (no answer on stdout) — most "+
				"likely this vendored plankton binary predates --trust-keys support on `reproductions` "+
				"and needs upgrading; the cockpit refuses to fall back to an unverified, forgeable count. "+
				"underlying error: %w", err)
	}
	res, perr := parseReproductionsJSON(out)
	if perr != nil {
		return nil, perr
	}
	// plankton answers "self-declared" when it had no trusted key to check against — an empty
	// trust-tier config produces an empty --trust-keys directory, and it then falls back to the
	// keyid each envelope claims about itself, which its author wrote. Returning that count under a
	// field named verifiedSigners would be the forgeable number this whole path exists to refuse,
	// with a label saying the opposite. plankton warns on stderr; a warning beside a wrong number is
	// not enough, because the number is what gets read.
	if res.Trust != "verified" {
		return nil, fmt.Errorf(
			"plankton answered with a %s count, not a verified one — this repo's trust tiers name no key "+
				"that could verify anything, so ↻N would be over keyids the records claim about themselves. "+
				"Configure trust.tiers", res.Trust)
	}
	res.Warning = errText
	return res, nil
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
// It reads plankton's --json verdict rather than the sentence it prints. That closes a gap this
// wrapper previously documented as unclosable: `reproduces` exits 1 both for a genuine non-match
// and for a usage error, so the two could not be told apart, and a broken invocation would have
// been reported to the caller as "these outputs differ". With --json they are distinguishable
// without relying on the exit code at all — a real verdict comes with a parseable answer, a usage
// error comes with none.
func (r *Runner) Reproduces(ctx context.Context, refHash, candHash, via string) (*ReproducesResult, error) {
	args := []string{"reproduces", refHash, candHash, "--json"}
	if via != "" {
		args = append(args, "--via", via)
	}
	out, errText, err := r.exec(ctx, "plankton", args...)

	var res ReproducesResult
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); jerr != nil {
		// No verdict. Exit 1 alone would have looked exactly like "they do not match".
		return nil, fmt.Errorf("plankton reproduces gave no verdict: %w\n%s", errOr(err, jerr), errText)
	}
	return &res, nil
}

func errOr(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

// VerifyFoton verifies a plankton envelope/id against an explicit pubkey — never against the
// envelope's own declared keyid.
//
// Confirmed live against the reference binary: exit 0 = "VALID", exit 2 = "UNVERIFIED - WRONG
// KEY" (this key genuinely did not sign the record — the expected, non-error outcome while trying
// each configured trust-tier pubkey in turn), and any OTHER non-zero exit (e.g. exit 1, prefixed
// "error: ...") is a real failure: pubkey file not found, record not found, malformed input.
// Collapsing all three into "ok=false, err=nil" — the previous behavior — silently turns a broken
// trust-tier config (e.g. a typo'd .pub path) into what looks like a legitimate "untrusted signer"
// verdict, with no error ever surfaced to tell the operator their config itself is broken.
func (r *Runner) VerifyFoton(ctx context.Context, idOrFile, pubkeyPath string) (ok bool, output string, err error) {
	out, err := r.plankton(ctx, "verify", idOrFile, pubkeyPath)
	if err == nil {
		return true, out, nil
	}
	if isVerifyMismatch(err) || isVerifyStructural(err) {
		return false, out, nil
	}
	return false, out, err
}

// isVerifyMismatch reports whether err represents plankton/nekton verify's own exit code 2 — a
// genuine, expected "this key did not sign the record" outcome — as opposed to any other failure
// (missing file, bad input, binary crash), which callers must treat as a real error.
func isVerifyMismatch(err error) bool {
	return verifyExitCode(err) == 2
}

// isVerifyStructural reports the kernel's exit 3: the signature IS genuine, and the record is still
// one `add` refuses — a malformed predicate, a seed carrying the wrong genesis flag, a payload that
// will not parse at all.
//
// It is a verdict about the RECORD, so it belongs with exit 2 rather than with exit 1. Exit 1 stays
// a hard error and must: it is an operational failure — an unreadable pubkey path, bad hex — and
// treating it as "this signer isn't trusted" is a bug this package already had once, where a typo'd
// .pub path read exactly like a legitimate exclusion.
//
// Reaching it matters more than it used to. kton tightened `nekton verify` so a payload that cannot
// be parsed is a structural failure rather than a silent exit 0, which means a claim accepted by an
// OLDER kernel can now answer exit 3 — and this cockpit reads stores it did not write. Left in the
// exit-1 bucket, one such record would fail every query that touched it rather than being excluded
// from the answer, which is a single record denying every answer about the rest.
func isVerifyStructural(err error) bool {
	return verifyExitCode(err) == 3
}

// verifyExitCode returns the process exit code behind err, or -1 when err is not an exit status.
// -1 is never a kernel verdict, so a non-ExitError can never be mistaken for one.
func verifyExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}

// --- nekton ---

// Annotate runs `nekton annotate` from a template and returns the printed claim id (nekton
// prints the claim's registry id on `--add`; callers should follow up with About to confirm
// registration, mirroring the manual workflow's explicit confirm step).
//
// --print-id is the same contract `plankton author` uses: the bare id is the only thing on stdout,
// every human line goes to stderr. Until kton 0.2's #56 added it, annotate printed four lines of
// prose to STDOUT with the id embedded among them, and this had to scrape the "indexed claim" line
// back out. Say still confirms registration by querying the claim back — the parsing went away, the
// confirmation did not.
func (r *Runner) Annotate(ctx context.Context, subject, template string, sets map[string]string, signKey string, chain *Chain) (string, error) {
	args := []string{"annotate", subject, "--template", template}
	for k, v := range sets {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	// Both or neither: the substrate refuses a scoped claim that carries no prev, so passing one
	// without the other would produce a signed claim the store then declines.
	if chain != nil {
		args = append(args, "--scope", chain.Scope, "--prev", chain.Prev)
	}
	args = append(args, "--sign", signKey, "--add", "--print-id")
	out, err := r.nekton(ctx, args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if !claimIDRe.MatchString(id) {
		return "", fmt.Errorf("nekton annotate --print-id did not return a claim id; stdout was:\n%s", out)
	}
	return id, nil
}

// claimIDRe is the whole of what --print-id may put on stdout. Matching the entire string, not
// searching within it, is the point: anything else there means the contract did not hold.
var claimIDRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// About lists claims about a subject (hash or URI) — used both to serve `ask` and to confirm a
// `say` registered. It reads nekton's --json mode rather than its prose (upstream #39): the prose
// carries only the id, predicate and declared signer, so the claim's actual content — the object,
// i.e. what was said — was unreachable through it.
func (r *Runner) About(ctx context.Context, subject string) ([]ClaimAxis, error) {
	out, err := r.nekton(ctx, "about", subject, "--json")
	if err != nil {
		return nil, err
	}
	return parseClaimsJSON(out)
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
	out, err := r.nekton(ctx, "by", string(axis), value, "--json")
	if err != nil {
		return nil, err
	}
	return parseClaimsJSON(out)
}

// Templates lists available claim templates, or shows one template's fields with show != "".
func (r *Runner) Templates(ctx context.Context, show string) (string, error) {
	if show == "" {
		return r.nekton(ctx, "templates")
	}
	return r.nekton(ctx, "templates", "--show", show)
}

// VerifyClaim verifies a nekton claim envelope/id against an explicit pubkey. Same exit-code
// convention as VerifyFoton (confirmed live: 0 = valid, 2 = wrong-key mismatch, anything else is
// a real error) — see that function's comment for why the distinction matters.
func (r *Runner) VerifyClaim(ctx context.Context, idOrFile, pubkeyPath string) (ok bool, output string, err error) {
	out, err := r.nekton(ctx, "verify", idOrFile, pubkeyPath)
	if err == nil {
		return true, out, nil
	}
	if isVerifyMismatch(err) || isVerifyStructural(err) {
		return false, out, nil
	}
	return false, out, err
}

// --- verification material (kton §8.1) ---

// StoredMaterial is one piece of evidence as the kernel hands it back.
//
// The kernel's `--json` includes a `verified` field that is ALWAYS false, and deliberately so: it
// stores material without evaluating it. That field is not mapped here, because carrying it would
// invite a caller to read a kernel-side verdict where none exists. Whether any of this checks out
// is decided by internal/material, on this side.
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
	_, _, err := r.exec(ctx, bin, "attach", id, "--scheme", scheme, "--media", mediaType, "--file", file)
	return err
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
	out, _, err := r.exec(ctx, bin, "material", id, "--json")
	if err != nil {
		return nil, err
	}
	var read struct {
		Material []StoredMaterial `json:"material"`
	}
	if err := json.Unmarshal([]byte(out), &read); err != nil {
		return nil, fmt.Errorf("could not read %s material %s: %w\n%s", bin, id, err, out)
	}
	return read.Material, nil
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
	out, _, err := r.exec(ctx, "nekton", "head", scopeID, "--json")
	if err != nil {
		return nil, err
	}
	var h ScopeHead
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &h); jerr != nil {
		return nil, fmt.Errorf("could not read the head of scope %s: %w\n%s", scopeID, jerr, out)
	}
	if len(h.Heads) == 0 {
		return nil, fmt.Errorf("nekton head reported scope %s with no head at all; refusing to guess one", scopeID)
	}
	return &h, nil
}

// ScopeChain returns every claim this registry holds that names scopeID, in the order the registry
// received them.
//
// Reading order from the registry rather than by walking prev is deliberate: a scope's claims live
// in one append-only file, so arrival order is a fact the store already has, while the prev links
// are the separate, independently checkable statement about what each writer believed preceded
// them. Keeping the two apart is what makes a disagreement between them visible at all.
func (r *Runner) ScopeChain(ctx context.Context, scopeID string) ([]ScopeClaim, error) {
	out, _, err := r.exec(ctx, "nekton", "records", "--json")
	if err != nil {
		return nil, fmt.Errorf("reading claims for scope %s: %w", scopeID, err)
	}
	recs, err := parseRecordsJSON(out)
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(recs)
	if err != nil {
		return nil, err
	}
	all, err := parseClaimsJSON(string(blob))
	if err != nil {
		return nil, err
	}
	envelopes := map[string]json.RawMessage{}
	for _, rec := range recs {
		envelopes[rec.ClaimID] = rec.Envelope
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
// "unresolvable" would swallow the operational errors §9.1 insists stay loud.
type ScopeClaim struct {
	ClaimAxis
	Envelope json.RawMessage
}

// Seed reports the scope's genesis statement — who opened it, when, under what name, and the
// `responsible` identities kton §7.4 lets a seed name. Membership is FIXED BY THE SEED, so this is
// where "who was supposed to write here" is written down; the kernel stores it and interprets none
// of it, and the reference implementation emits no `responsible` at all.
func (r *Runner) Seed(ctx context.Context, scopeID string) (*ScopeSeed, error) {
	out, _, err := r.exec(ctx, "nekton", "show", scopeID, "--json")
	if err != nil {
		return nil, err
	}
	var raw struct {
		ClaimID       string `json:"claimId"`
		PredicateType string `json:"predicateType"`
		Predicate     struct {
			Scope   string `json:"scope"`
			Genesis bool   `json:"genesis"`
			By      string `json:"by"`
			When    string `json:"when"`
			// Parent is a term reference, not a bare string: the wire carries {"hash": "sha256:…"}.
			// Decoding it as a string silently yields "" and a scope looks parentless — which is the
			// difference between "cannot be sealed" and "is sealed somewhere you did not look".
			Parent      struct{ Hash string } `json:"parent,omitempty"`
			Responsible []string              `json:"responsible,omitempty"`
		} `json:"predicate"`
	}
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); jerr != nil {
		return nil, fmt.Errorf("could not read the seed of scope %s: %w\n%s", scopeID, jerr, out)
	}
	if !raw.Predicate.Genesis {
		return nil, fmt.Errorf("%s is not a scope seed: its statement does not carry genesis", scopeID)
	}
	return &ScopeSeed{
		ID: raw.ClaimID, Name: raw.Predicate.Scope, By: raw.Predicate.By,
		When: raw.Predicate.When, Parent: raw.Predicate.Parent.Hash,
		Responsible: raw.Predicate.Responsible,
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
// gap §9.6 otherwise has to state as a limit — tail truncation is undetectable IN-BAND, and a seal
// is the out-of-band record that detects it.
func (r *Runner) SealScope(ctx context.Context, childScope, childHead, parentScope, parentHead, signKey string) (string, error) {
	// `by` is the signer's own keyid and `when` the moment of sealing. Both are covered by the
	// claim id, and `when` is what distinguishes one seal of a scope from the next: sealing is
	// meant to be repeated, so two seals at the same head must still be two records.
	keyid, err := r.KeyID(ctx, trimKeySuffix(signKey))
	if err != nil {
		return "", fmt.Errorf("reading the keyid to seal under: %w", err)
	}
	spec := map[string]any{
		"subject":   []map[string]string{{"hash": childScope}},
		"predicate": SealPredicate,
		"object":    map[string]any{"hash": childHead},
		"by":        "key:" + keyid,
		"when":      time.Now().UTC().Format(time.RFC3339),
		"scope":     parentScope,
		"prev":      parentHead,
	}
	blob, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "cockpit-seal-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	specPath := filepath.Join(dir, "seal.json")
	if err := os.WriteFile(specPath, blob, 0o600); err != nil {
		return "", err
	}
	out, _, err := r.exec(ctx, "nekton", "claim", specPath, signKey, "--add", "--print-id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if !claimIDRe.MatchString(id) {
		return "", fmt.Errorf("nekton claim --print-id did not return a claim id; stdout was:\n%s", out)
	}
	return id, nil
}

// SeedScope opens a scope and returns its id. An operator action, never a verb: a scope exists
// before the session that writes into it.
func (r *Runner) SeedScope(ctx context.Context, name, signKey, parentScope string) (string, error) {
	args := []string{"seed", name, "--sign", signKey, "--add", "--print-id"}
	if parentScope != "" {
		args = append(args, "--parent", parentScope)
	}
	out, _, err := r.exec(ctx, "nekton", args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if !claimIDRe.MatchString(id) {
		return "", fmt.Errorf("nekton seed --print-id did not return a scope id; stdout was:\n%s", out)
	}
	return id, nil
}

// trimKeySuffix turns keys/x.key into keys/x.pub — the config names the private half, and the
// public half sits beside it under the same stem.
func trimKeySuffix(privateKeyPath string) string {
	return strings.TrimSuffix(privateKeyPath, ".key") + ".pub"
}
