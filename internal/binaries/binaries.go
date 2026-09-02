// Package binaries shells out to the vendored plankton/nekton binaries. It never reimplements
// any of their logic (canonicalization, hashing, signing, registry, verification) — it only
// invokes them with the cockpit's resolved config and parses their plain-text stdout.
package binaries

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

// Reproduces checks two output hashes for L0/L1 equivalence (exit 0 = pass); via, if non-empty,
// names the shared normalizer.
func (r *Runner) Reproduces(ctx context.Context, refHash, candHash, via string) (ok bool, output string, err error) {
	args := []string{"reproduces", refHash, candHash}
	if via != "" {
		args = append(args, "--via", via)
	}
	out, err := r.plankton(ctx, args...)
	if err != nil {
		return false, out, nil // non-zero exit = not reproduced, not a tool failure
	}
	return true, out, nil
}

// VerifyFoton verifies a plankton envelope/id against an explicit pubkey — never against the
// envelope's own declared keyid.
func (r *Runner) VerifyFoton(ctx context.Context, idOrFile, pubkeyPath string) (ok bool, output string, err error) {
	out, err := r.plankton(ctx, "verify", idOrFile, pubkeyPath)
	if err != nil {
		return false, out, nil
	}
	return true, out, nil
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
func (r *Runner) Annotate(ctx context.Context, subject, template string, sets map[string]string, signKey string) (string, error) {
	args := []string{"annotate", subject, "--template", template}
	for k, v := range sets {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
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

// VerifyClaim verifies a nekton claim envelope/id against an explicit pubkey.
func (r *Runner) VerifyClaim(ctx context.Context, idOrFile, pubkeyPath string) (ok bool, output string, err error) {
	out, err := r.nekton(ctx, "verify", idOrFile, pubkeyPath)
	if err != nil {
		return false, out, nil
	}
	return true, out, nil
}
