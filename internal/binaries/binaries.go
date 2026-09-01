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
	args = append(args, "--cmd", in.Cmd, "--sign", in.SignKey, "--add", "--print-id")

	out, err := r.plankton(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Hash returns the content hash of a file as `plankton hash` prints it.
func (r *Runner) Hash(ctx context.Context, file string) (string, error) {
	return r.plankton(ctx, "hash", file)
}

// Show prints a foton's descriptor (command, inputs, outputs).
func (r *Runner) Show(ctx context.Context, idOrFile string) (string, error) {
	return r.plankton(ctx, "show", idOrFile)
}

// Producer, Uses, Lineage, Reproductions are read-only graph queries by hash. Producer/Uses/
// Lineage share plankton's degraded-registry-read code path, which can print an "INCOMPLETE"
// warning to stderr even on a clean exit — planktonQuery (not plankton) preserves that warning.
func (r *Runner) Producer(ctx context.Context, hash string) (string, error) {
	return r.planktonQuery(ctx, "producer", hash)
}

func (r *Runner) Uses(ctx context.Context, hash string) (string, error) {
	return r.planktonQuery(ctx, "uses", hash)
}

func (r *Runner) Lineage(ctx context.Context, hash string) (string, error) {
	return r.planktonQuery(ctx, "lineage", hash)
}

// Reproductions returns plankton's ↻N report for outputHash, verified against this repo's
// configured trust tiers via --trust-keys — never plankton's bare, self-declared signer count.
// Without --trust-keys the count is forgeable (a relabeled keyid inflates ↻N and mis-attributes a
// reproduction to a party who never signed); the cockpit already holds exactly the keys that
// should count; it must pass them.
//
// The binary exits non-zero for the legitimate "0 distinct producers" case (not just for real
// failures), and plankton always prints that answer to stdout before exiting — so a non-zero exit
// with non-empty stdout is treated as a valid, informational zero-count result, same convention
// as Reproduces below. A non-zero exit with EMPTY stdout means the command itself rejected the
// call outright (most plausibly: the vendored plankton binary predates --trust-keys support on
// `reproductions`, which kton 0.2 does provide but 0.1 did not) rather than answering "zero
// producers", and is surfaced as a real, actionable error instead of being handed to Claude as if
// it were a valid count.
func (r *Runner) Reproductions(ctx context.Context, outputHash string) (string, error) {
	dir, cleanup, err := trustKeysDir(r.cfg)
	if err != nil {
		return "", fmt.Errorf("materializing trust keys for reproductions: %w", err)
	}
	defer cleanup()

	out, err := r.plankton(ctx, "reproductions", "--trust-keys", dir, outputHash)
	if err != nil && out == "" {
		return "", fmt.Errorf(
			"plankton rejected `reproductions --trust-keys` outright (no answer on stdout) — most "+
				"likely this vendored plankton binary predates --trust-keys support on `reproductions` "+
				"and needs upgrading; the cockpit refuses to fall back to an unverified, forgeable count. "+
				"underlying error: %w", err)
	}
	return out, nil
}

// trustKeysDir materializes every pubkey configured across this repo's trust tiers into a fresh
// flat directory of *.pub files — the shape `--trust-keys <dir>` expects (raw hex Ed25519, one key
// per *.pub file, loaded non-recursively). Callers must invoke the returned cleanup once done.
func trustKeysDir(cfg *config.Config) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "cockpit-trust-keys-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(dir) }

	i := 0
	for pubkeyPath := range cfg.TierPubkeys() {
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
// Unlike `plankton author --print-id`, `nekton annotate` has no machine-readable mode: it prints
// four lines of human prose to STDOUT (not stderr), the id embedded among them. So the id is
// parsed out here rather than returned raw — returning the whole block would hand the caller a
// paragraph where it expects an id. Filed upstream against kton-protocol/kton alongside #39,
// which is the same problem on the read side (about/by print prose too).
func (r *Runner) Annotate(ctx context.Context, subject, template string, sets map[string]string, signKey string) (string, error) {
	args := []string{"annotate", subject, "--template", template}
	for k, v := range sets {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, "--sign", signKey, "--add")
	out, err := r.nekton(ctx, args...)
	if err != nil {
		return "", err
	}
	// Anchored on the "indexed claim" line specifically, not on the earlier "claim <id>" line
	// that merely reports what was authored: only the indexed line means --add actually ingested
	// it into the registry. If ingestion did not happen, there is no id to report and this fails
	// closed rather than handing back an id for a claim no query will find.
	m := indexedClaimRe.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("nekton annotate did not report an indexed claim id; output was:\n%s", out)
	}
	return m[1], nil
}

// indexedClaimRe matches nekton's own registration line, e.g.
// `indexed claim sha256:98a1…  (registry now holds 1 claims)`.
var indexedClaimRe = regexp.MustCompile(`(?m)^indexed claim (sha256:[0-9a-f]{64})\b`)

// About lists claims about a subject (hash or URI) — used both to serve `ask` and to confirm a
// `say` registered.
func (r *Runner) About(ctx context.Context, subject string) (string, error) {
	return r.nekton(ctx, "about", subject)
}

// By lists claims by signer keyid, predicate, or object.
func (r *Runner) By(ctx context.Context, value string) (string, error) {
	return r.nekton(ctx, "by", value)
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
