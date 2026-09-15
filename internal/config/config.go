// Package config loads and validates cockpit.config.json — the static ceiling the cockpit applies
// on every call. It is never written by the cockpit itself, and no tool exposes it: Claude cannot
// read or change it through publish, say or ask.
//
// One path had to be closed to keep that true. With execution configured the cockpit mounts the
// repository into a container and runs a Claude-supplied command there, and the config sits inside
// that mount. It is masked — see internal/container's maskArgs — along with the keys, the binaries,
// the registry and .git. Without that, "no tool reaches it" would have been a statement about the
// tool surface that the tool surface itself had made untrue.
package config

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Mode names how this cockpit is bound to a location, and therefore what the anti-wrong-folder
// guard checks. Absent means ModeGit, so every existing config keeps its behaviour.
const (
	ModeGit   = "git"
	ModeLocal = "local"
)

// RepoRef binds a cockpit to exactly one place. Which fields are required depends on Mode.
type RepoRef struct {
	// Mode is "git" (default) or "local".
	Mode string `json:"mode,omitempty"`
	// Owner and Name are the GitHub owner/repo, required in git mode. Every call verifies them
	// against the repo's actual `git remote get-url origin`.
	Owner string `json:"owner,omitempty"`
	Name  string `json:"name,omitempty"`
	// Root is the absolute path this config belongs to, required in local mode. Every call verifies
	// that the config really is where it says it is.
	Root string `json:"root,omitempty"`
}

// IsLocal reports whether this cockpit runs without git.
func (r RepoRef) IsLocal() bool { return r.Mode == ModeLocal }

type Paths struct {
	PlanktonDir  string `json:"plankton_dir"`
	NektonDir    string `json:"nekton_dir"`
	TemplatesDir string `json:"templates_dir"`
	KeysDir      string `json:"keys_dir"`
	BinDir       string `json:"bin_dir"`
}

type Identity struct {
	SessionID   string `json:"session_id"`
	PlanktonKey string `json:"plankton_key"`
	NektonKey   string `json:"nekton_key"`
}

type Verbs struct {
	Publish bool `json:"publish"`
	Say     bool `json:"say"`
	Ask     bool `json:"ask"`
}

type Claims struct {
	AllowedTemplates []string `json:"allowedTemplates"`
}

type Trust struct {
	Tiers map[string][]string `json:"tiers"`
}

// Environment pins WHICH execution environment produced this repo's fotons.
//
// Deliberately config, not a tool argument. Both fields are COVERED — they ride in the foton's
// descriptor into its id, so they are part of what a reproduction commits to re-executing. A value
// supplied per-call by Claude would therefore be a self-declared, unverified assertion about which
// environment ran, baked into the record's identity; the same class of thing this project refuses
// everywhere else (see how say computes the reproduction level itself). The human operator knows
// which environment their cockpit runs in; Claude does not.
type Environment struct {
	// Spectrum is the qualified env-spectrum id (`plankton author --environment`), which must be a
	// content hash. It QUALIFIES an environment; it does not name an exact one.
	Spectrum string `json:"spectrum,omitempty"`
	// EnvRef is the exact execution environment (`plankton author --env-ref`): an OCI image digest,
	// a nix store path, a run-server id. The substrate stores it without interpreting it.
	EnvRef string `json:"envRef,omitempty"`
}

// Anchor controls whether records are witnessed in a Sigstore Rekor transparency log.
//
// A signature says who signed. It does not say WHEN, and it does not stop the signer from later
// producing a different record and claiming that one was the original. An anchor adds an
// independent, append-only witness: Rekor attests that this exact record existed by a given time,
// and the proof is stored beside the record as verification material (kton §8.1).
//
// Off by default, and that default is not timidity: anchoring PUBLISHES THE RECORD, permanently.
//
// What goes to Rekor is the whole DSSE envelope — sigstore/rekor.go sets
// `ProposedContent.Envelope` to the serialised envelope, not a digest of it. The payload inside is
// the in-toto statement, so the log receives the command that was run, every input and output path
// and hash, the pinned environment, and the commit-pinned permalinks, which name the GitHub
// owner/repo. For a PRIVATE repository that means its name, its file layout and its commands become
// public and stay public; the file CONTENTS do not, since only their hashes are recorded.
//
// The entry cannot be withdrawn. Turn this on for work that is meant to be public, or where an
// independent witness of WHEN is worth that disclosure.
type Anchor struct {
	// Enabled turns it on for every record this cockpit writes.
	Enabled bool `json:"enabled,omitempty"`
	// RekorURL overrides the public log. Empty means the well-known public Rekor.
	RekorURL string `json:"rekorUrl,omitempty"`
	// RekorPubkey pins the key the log's Signed Entry Timestamp is checked against — a PEM file
	// path, or inline PEM. Required whenever RekorURL is set: an endpoint that both issues and
	// verifies its own SET verifies nothing, so a custom log without a pinned key could fabricate
	// entries that self-verify. The kernel refuses that case; this refuses it earlier, at load.
	RekorPubkey string `json:"rekorPubkey,omitempty"`
}

// Material is external evidence this cockpit attaches to every record it writes, plus the roots it
// is able to check such evidence against.
//
// kton §8.1 keeps verification material deliberately open, and `plankton attach` says why in as
// many words: an unknown scheme is carried rather than rejected, because "refusing unknown evidence
// would make this list a protocol version". This block inherits that stance exactly. It prescribes
// no key form, no issuer, no algorithm and no scheme token — it says what to carry, and separately
// what this repo can evaluate.
//
// Those two are kept apart on purpose. Evidence named here always travels with the record, whether
// or not anyone here can read it; the kernel stores it as opaque bytes and never evaluates it. What
// this cockpit then checks is a smaller set, and `cockpit_ask` reports the difference per item
// rather than blurring it — carried-but-unevaluated is a real and useful state, and silently
// counting it as verified, or silently dropping it, would each be a lie in a different direction.
//
// None of this touches Trust.Tiers. A tier decides whether a record COUNTS here; material says who
// a key belongs to and when the record existed. Three orthogonal questions, none prescribing the
// others.
type Material struct {
	// Attach rides along with every record this cockpit writes — one entry per piece of evidence.
	Attach []Attachment `json:"attach,omitempty"`

	// X509Roots are PEM trust anchors that an attached certificate's chain is verified against.
	//
	// Empty is not a failure and not a default-to-system-roots: with no roots configured a
	// certificate is still attached and still reported, as CARRIED rather than VERIFIED. Falling
	// back to the host's root store would make the verdict depend on which machine happened to run
	// the query, which is exactly the kind of ambient answer this project refuses elsewhere.
	X509Roots []string `json:"x509Roots,omitempty"`
}

// Attachment is one piece of evidence to carry, named the way `plankton attach` takes it.
type Attachment struct {
	// Scheme is the kton §8.1 token that says what produced Material. The listed tokens are
	// sigstore-bundle, rekor-entry, rfc3161, cms-detached, jades and pgp-detached — but the list is
	// open and an unlisted one is accepted here for the same reason the kernel accepts it.
	Scheme string `json:"scheme"`

	// MediaType says how to read the bytes. Required here even for a scheme the kernel has a default
	// for: the kernel's default table is a convenience whose contents can change, and a cockpit that
	// depended on it would carry a different media type after a kernel upgrade without anything in
	// this repo having changed. Naming it makes an unlisted scheme need no special case either.
	MediaType string `json:"mediaType"`

	// File is the repo-relative path to the evidence bytes.
	File string `json:"file"`
}

// Union controls whether this repo also publishes its aggregate as committed files, so the graph is
// reachable online without anyone running `cockpit show` locally.
//
// The files are the same three a viewer fetches — union.json, keys.json, names.json — regenerated
// after every record this cockpit writes and committed alongside it. A viewer is then pointed at
// their raw URLs, or at GitHub Pages if the repo serves them.
type Union struct {
	// Publish turns it on. Off by default: it commits a derived file on every publish, which is a
	// real cost in diff noise for a repo nobody views online.
	Publish bool `json:"publish,omitempty"`
	// Dir is where they go, repo-relative. Defaults to docs/data, which is what GitHub Pages serves
	// from without configuration.
	Dir string `json:"dir,omitempty"`
}

// DirOrDefault is where the union files are written.
func (u Union) DirOrDefault() string {
	if u.Dir == "" {
		return "docs/data"
	}
	return u.Dir
}

// Git controls whether the cockpit commits and pushes on Claude's behalf. Both default to on;
// omitting the block entirely keeps the behaviour every existing repo has.
//
// This does NOT touch the anti-wrong-folder guard. That check reads the repo's own `origin` remote
// on every call and is the reason this project exists; it applies whether or not the cockpit writes
// anything. Turning commits off makes the cockpit stop WRITING to git, never stop CHECKING which
// repo it is in.
type Git struct {
	// Commit, when false, means the cockpit signs and registers records without committing the
	// files they describe. The foton is still valid — a foton never implied its bytes were
	// published anywhere — but it carries no locators, so a peer has nowhere to fetch and re-hash
	// them from.
	Commit *bool `json:"commit,omitempty"`
	// Push, when false, commits locally without pushing. Permalinks are still built, because the
	// commit they pin is real and stable; they simply do not resolve until someone pushes.
	Push *bool `json:"push,omitempty"`
}

// CommitEnabled and PushEnabled default to true, so an absent block behaves as before. Turning
// commits off turns pushes off with them — there would be nothing to push — so `commit: false`
// alone is a complete statement and needs no second flag.
func (g Git) CommitEnabled() bool { return g.Commit == nil || *g.Commit }
func (g Git) PushEnabled() bool {
	return g.CommitEnabled() && (g.Push == nil || *g.Push)
}

// Execution turns cockpit_publish from recording a command into running one, inside a pinned
// container. Off unless Image is set.
//
// The point is that the environment stops being a declaration. Config alone can only assert "we run
// in image X"; nothing checks the command actually ran there. When the cockpit runs it, the string
// handed to the container runtime and the string pinned into the foton are the same string, so what
// ran and what is recorded cannot differ. See ADR-003.
type Execution struct {
	// Engine is the container runtime executable. Empty means "docker".
	Engine string `json:"engine,omitempty"`
	// Image is the digest-pinned OCI reference to run in, e.g. oci://ghcr.io/org/x@sha256:...
	// Setting it is what enables execution.
	Image string `json:"image,omitempty"`
	// Network allows the container to reach the network. Default false: a run that reaches the
	// internet depended on something the foton does not pin, so its environment claim is weaker —
	// and inside claude-science's sandbox, where the cockpit is Claude's entire surface, it is also
	// the cheapest way out of it. Publishing records when this was on.
	Network bool `json:"network,omitempty"`
}

// CommitEnabled and PushEnabled answer for the whole configuration, not just the git block: with no
// git repository there is nothing to commit to, so local mode turns both off structurally rather
// than by asking the operator to also set the flags.
func (r Raw) CommitEnabled() bool { return !r.Repo.IsLocal() && r.Git.CommitEnabled() }
func (r Raw) PushEnabled() bool   { return r.CommitEnabled() && r.Git.PushEnabled() }

// PinnedEnvRef is the exact execution environment to record in a foton. When the cockpit runs the
// command itself, that is by definition the image it ran it in — the config cannot disagree,
// because validateExecution refuses a config where the two differ.
func (r Raw) PinnedEnvRef() string {
	if r.Execution.Enabled() {
		return r.Execution.Image
	}
	return r.Environment.EnvRef
}

// Enabled reports whether this repo runs published commands rather than recording them.
func (e Execution) Enabled() bool { return e.Image != "" }

// EngineOrDefault is the runtime executable to invoke.
func (e Execution) EngineOrDefault() string {
	if e.Engine == "" {
		return "docker"
	}
	return e.Engine
}

// ImageRef strips the oci:// scheme, which pins the reference in a foton but is not what a
// container runtime accepts on its command line.
func (e Execution) ImageRef() string { return strings.TrimPrefix(e.Image, "oci://") }

type Reproduction struct {
	// RequiredLevel is the level a `reproduces` claim must reach before it is written at all.
	RequiredLevel string `json:"requiredLevel"`
	Normalizer    string `json:"normalizer"`
	// MinReproductions is how many independent, VERIFIED producers a record must have before this
	// repo will let it be the basis of its own work — the corpus of a publish.
	//
	// The difference from RequiredLevel is which question it answers. RequiredLevel is about work
	// this repo did: did my re-run actually match. MinReproductions is about work somebody ELSE did:
	// how many independent parties have produced these same bytes before I build on them. A result
	// nobody has reproduced may be perfectly correct; it has simply not been corroborated, and a
	// repo may reasonably decline to stand on it.
	//
	// Zero, the default, requires nothing. The count is always the verified one (SPEC §9.3): a
	// self-declared ↻N would make this threshold satisfiable by relabelling a keyid.
	MinReproductions int `json:"minReproductions,omitempty"`
}

// Raw is the on-disk shape of cockpit.config.json, with paths still relative to RepoRoot.
type Raw struct {
	Repo         RepoRef      `json:"repo"`
	Paths        Paths        `json:"paths"`
	Identity     Identity     `json:"identity"`
	Verbs        Verbs        `json:"verbs"`
	Claims       Claims       `json:"claims"`
	Trust        Trust        `json:"trust"`
	Reproduction Reproduction `json:"reproduction"`
	Environment  Environment  `json:"environment,omitempty"`
	Execution    Execution    `json:"execution,omitempty"`
	Git          Git          `json:"git,omitempty"`
	Union        Union        `json:"union,omitempty"`
	Anchor       Anchor       `json:"anchor,omitempty"`
	Material     Material     `json:"material,omitempty"`
}

// Config is the loaded, validated, path-resolved configuration for one cockpit invocation. Every
// path is absolute and already verified to exist. RepoRoot is the git repository root the config
// was loaded from and the repo/remote match was checked against.
type Config struct {
	Raw      Raw
	RepoRoot string

	PlanktonDir  string
	NektonDir    string
	TemplatesDir string
	KeysDir      string
	BinDir       string

	PlanktonKey string
	NektonKey   string
}

// Load resolves the git repository root containing startDir, loads cockpit.config.json from that
// root only (never an ancestor), validates it, and hard-fails if the repo's own git remote does
// not match the configured owner/name. This is the anti-wrong-folder guard: every MCP tool call
// goes through Load again, so the cockpit cannot silently keep acting against a stale or mistaken
// working directory.
//
// If the COCKPIT_REPO_DIR environment variable is set, it replaces startDir. This exists for MCP
// client runtimes that give no way to pin a spawned local command's working directory (e.g. a
// managed sandbox with no shell available to `cd` first) — the human operator sets it once, in
// the same place they'd configure the command itself, so it is not something Claude can reach or
// change at runtime. This is not the kind of ambient env var this guard exists to distrust
// (PLANKTON_DIR/NEKTON_DIR/etc. are still always derived from the resolved repo root, never from
// the environment) — it only answers "which repo," never "which registry paths within it."
func Load(ctx context.Context, startDir string) (*Config, error) {
	if envDir := os.Getenv("COCKPIT_REPO_DIR"); envDir != "" {
		startDir = envDir
	}

	// The config has to be found before its mode can be read, so the search is the same either way:
	// walk up to the nearest cockpit.config.json. Whether that location is legitimate is then
	// decided by the mode — in git mode it must be the repository root, in local mode it must be the
	// path the config itself declares. Finding a config somewhere is never on its own enough.
	cfgDir, err := findConfigDir(ctx, startDir)
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(cfgDir, "cockpit.config.json")

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("cockpit: could not read %s: %w", cfgPath, err)
	}
	var raw Raw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is not valid JSON: %w", cfgPath, err)
	}
	if err := validate(&raw); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is invalid: %w", cfgPath, err)
	}

	if err := checkTrustTierContents(cfgDir, &raw); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is invalid: %w", cfgPath, err)
	}

	if err := checkMaterialFiles(cfgDir, raw.Material); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is invalid: %w", cfgPath, err)
	}

	if err := checkBinding(ctx, cfgDir, raw.Repo); err != nil {
		return nil, fmt.Errorf("cockpit: refusing to act — %w", err)
	}

	repoRoot := cfgDir
	cfg := &Config{
		Raw:          raw,
		RepoRoot:     repoRoot,
		PlanktonDir:  filepath.Join(repoRoot, raw.Paths.PlanktonDir),
		NektonDir:    filepath.Join(repoRoot, raw.Paths.NektonDir),
		TemplatesDir: filepath.Join(repoRoot, raw.Paths.TemplatesDir),
		KeysDir:      filepath.Join(repoRoot, raw.Paths.KeysDir),
		BinDir:       filepath.Join(repoRoot, raw.Paths.BinDir),
		PlanktonKey:  filepath.Join(repoRoot, raw.Identity.PlanktonKey),
		NektonKey:    filepath.Join(repoRoot, raw.Identity.NektonKey),
	}
	return cfg, nil
}

// findConfigDir locates the cockpit.config.json that governs startDir, in exactly two places and no
// others: startDir itself, and — if startDir is inside a git repository — that repository's root.
//
// It used to walk up to the filesystem root and take the first config it found. That is a weaker
// rule than the one it replaced, and weaker in the direction that matters: the original design
// required the config AT the git root and refused otherwise, so a session in a directory with no
// config of its own could not bind to an ancestor's. Under an unbounded walk it can — including out
// of a git repository it is standing in, into a parent that has one. That is the incident this
// project exists for, reintroduced by a search.
//
// "The declared root proves it was not moved" does not answer it. It proves the config is where it
// says it belongs; it says nothing about whether that is the directory the session was meant to act
// in, which is the whole question.
//
// So: bounded. In git mode the config can only ever be at the repository root, which is the property
// the previous design had. In local mode it must be in the directory itself.
func findConfigDir(ctx context.Context, startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	if hasConfig(dir) {
		return dir, nil
	}
	// One step, and only to a place git itself names — never an arbitrary ancestor.
	if root, rerr := repoRootOf(ctx, dir); rerr == nil && hasConfig(root) {
		return root, nil
	}
	return "", fmt.Errorf(
		"cockpit: no cockpit.config.json in %s, and none at its git repository root — the config is "+
			"looked for in those two places only, never in an arbitrary parent directory", startDir)
}

func hasConfig(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "cockpit.config.json"))
	return err == nil
}

// checkBinding is the anti-wrong-folder guard. It answers one question — is this the place this
// config was written for — and refuses outright on any doubt. How it answers depends on the mode,
// because the two modes have different second opinions available:
//
//   - git: the repository's own `origin` remote must name the configured owner/repo, and the config
//     must sit at the repository root. The config and git's metadata are independent sources, so
//     this catches acting on a different repository than the one configured.
//   - local: there is no remote to disagree with, so the config's own declared absolute path is the
//     anchor and must equal where the config actually is. This catches a directory that was copied
//     or moved — the case the guard was originally built for, which was several sibling registries
//     under one parent rather than a different remote.
//
// Neither catches everything: git mode passes a duplicated clone, local mode passes two directories
// whose paths both look right. See ADR-004.
func checkBinding(ctx context.Context, cfgDir string, repo RepoRef) error {
	if repo.IsLocal() {
		// Local mode is for a directory with no git repository. Accepting it inside one that HAS an
		// origin would silently trade the stronger check for the weaker: the remote is an independent
		// second source, and the declared path is only the config agreeing with itself. A config that
		// turns that off by naming a mode, in a repo where it was available, is the kind of quiet
		// downgrade this guard exists to prevent.
		if root, err := repoRootOf(ctx, cfgDir); err == nil {
			if _, rerr := originURL(ctx, root); rerr == nil {
				return fmt.Errorf(
					"repo.mode is %q, but %s is a git repository with an origin remote — local mode would "+
						"drop the remote check for a weaker one. Use the default git mode here",
					ModeLocal, root)
			}
		}
		declared, err := filepath.Abs(repo.Root)
		if err != nil {
			return fmt.Errorf("repo.root %q is not a usable path: %w", repo.Root, err)
		}
		if !samePath(declared, cfgDir) {
			return fmt.Errorf(
				"location mismatch: cockpit.config.json says it belongs at %s, but it is at %s — refusing "+
					"to act against a directory this config was not written for", declared, cfgDir)
		}
		return nil
	}

	repoRoot, err := repoRootOf(ctx, cfgDir)
	if err != nil {
		return fmt.Errorf(
			"not inside a git repository (%s), and repo.mode is not %q: %w", cfgDir, ModeLocal, err)
	}
	if !samePath(repoRoot, cfgDir) {
		return fmt.Errorf(
			"cockpit.config.json is at %s but the git repository root is %s — the config must sit at the "+
				"repository root, so that what it binds is unambiguous", cfgDir, repoRoot)
	}
	return checkRemoteMatches(ctx, repoRoot, repo)
}

// samePath compares two absolute paths after resolving symlinks where possible, so a config reached
// through a symlinked parent is not mistaken for the wrong directory.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func validate(raw *Raw) error {
	var missing []string
	switch raw.Repo.Mode {
	case "", ModeGit:
		if raw.Repo.Owner == "" {
			missing = append(missing, "repo.owner")
		}
		if raw.Repo.Name == "" {
			missing = append(missing, "repo.name")
		}
	case ModeLocal:
		if raw.Repo.Root == "" {
			missing = append(missing, "repo.root (the absolute path this config belongs to — in local mode it is what the guard checks, since there is no remote to disagree with)")
		} else if !filepath.IsAbs(raw.Repo.Root) {
			return fmt.Errorf("repo.root must be an absolute path, got %q", raw.Repo.Root)
		}
	default:
		return fmt.Errorf("repo.mode %q is not one of %q, %q", raw.Repo.Mode, ModeGit, ModeLocal)
	}
	if raw.Paths.PlanktonDir == "" {
		missing = append(missing, "paths.plankton_dir")
	}
	if raw.Paths.NektonDir == "" {
		missing = append(missing, "paths.nekton_dir")
	}
	if raw.Identity.SessionID == "" {
		missing = append(missing, "identity.session_id")
	}
	if raw.Identity.PlanktonKey == "" {
		missing = append(missing, "identity.plankton_key")
	}
	if raw.Identity.NektonKey == "" {
		missing = append(missing, "identity.nekton_key")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missing, ", "))
	}
	if err := validateTrustTiers(raw); err != nil {
		return err
	}
	if err := validateEnvironment(raw.Environment); err != nil {
		return err
	}
	if err := validateExecution(raw.Execution, raw.Environment); err != nil {
		return err
	}
	if err := validateMaterial(raw.Material); err != nil {
		return err
	}
	if raw.Repo.IsLocal() && raw.Git.Commit != nil && *raw.Git.Commit {
		return fmt.Errorf("git.commit is true but repo.mode is %q — there is no git repository to commit to", ModeLocal)
	}
	if raw.Anchor.RekorURL != "" && raw.Anchor.RekorPubkey == "" {
		return fmt.Errorf(
			"anchor.rekorUrl names a custom log but anchor.rekorPubkey is empty — a log that issues its " +
				"own Signed Entry Timestamp and is then checked against its own key verifies nothing, so a " +
				"fabricated entry would self-verify. Pin the log's public key, or use the public Rekor by " +
				"leaving rekorUrl empty")
	}
	if raw.Anchor.RekorURL != "" && !raw.Anchor.Enabled {
		return fmt.Errorf("anchor.rekorUrl is set but anchor.enabled is not — nothing would be anchored anywhere")
	}
	if raw.Union.Dir != "" {
		// The union files are written into the repo and then committed, so this path is as
		// consequential as a publish path: `../..` escapes the repository, and `keys` or `.git`
		// aims at exactly what publish's denylist exists to keep out — with the difference that
		// nobody has to ask for it, since every record rewrites these files.
		if err := validateRepoRelativeDir(raw.Union.Dir, raw.Paths); err != nil {
			return fmt.Errorf("union.dir: %w", err)
		}
	}
	if raw.Union.Publish && !raw.CommitEnabled() {
		return fmt.Errorf(
			"union.publish is on but this repo does not commit — a published union is a committed file, so " +
				"there would be nowhere for it to go")
	}
	// Only the explicit contradiction is an error. Setting `commit: false` alone is fine and means
	// no push either; writing `push: true` next to it states something that cannot happen.
	if raw.Git.Push != nil && *raw.Git.Push && !raw.Git.CommitEnabled() {
		return fmt.Errorf("git.push is true but git.commit is false — there would be nothing to push")
	}
	return nil
}

// validateExecution refuses a configuration that cannot mean what it says.
func validateExecution(ex Execution, env Environment) error {
	if !ex.Enabled() {
		if ex.Network {
			return fmt.Errorf("execution.network is set but execution.image is not — nothing runs, so nothing is being allowed onto the network")
		}
		return nil
	}
	if !strings.HasPrefix(ex.Image, "oci://") {
		return fmt.Errorf("execution.image must be an oci:// reference, got %q", ex.Image)
	}
	if !strings.Contains(ex.Image, "@sha256:") {
		return fmt.Errorf(
			"execution.image %q has no digest — a tag names whatever it points at today, so running "+
				"\"in that image\" would pin nothing. Use oci://<image>@sha256:<digest>", ex.Image)
	}
	// A repo that says it runs in one environment and records another is not a configuration; it is
	// a false record waiting to be signed.
	if env.EnvRef != "" && env.EnvRef != ex.Image {
		return fmt.Errorf(
			"environment.envRef (%s) and execution.image (%s) disagree: the cockpit would run in one "+
				"environment and pin the other into the foton. Set only execution.image — it is used for both",
			env.EnvRef, ex.Image)
	}
	return nil
}

// validateRepoRelativeDir refuses a directory the cockpit must not write into or commit from.
func validateRepoRelativeDir(dir string, paths Paths) error {
	if filepath.IsAbs(dir) {
		return fmt.Errorf("%q is an absolute path; it must be repo-relative", dir)
	}
	clean := filepath.ToSlash(filepath.Clean(dir))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%q escapes the repository", dir)
	}
	if strings.HasPrefix(clean, "-") {
		return fmt.Errorf("%q starts with a dash and could be read by git as a flag rather than a path", dir)
	}
	for _, forbidden := range []string{".git", paths.KeysDir, paths.BinDir, paths.PlanktonDir, paths.NektonDir} {
		if forbidden == "" {
			continue
		}
		f := filepath.ToSlash(filepath.Clean(forbidden))
		if clean == f || strings.HasPrefix(clean, f+"/") {
			return fmt.Errorf("%q is inside %s, which the cockpit must not overwrite", dir, forbidden)
		}
	}
	return nil
}

// validateTrustTiers refuses a tier entry that is a PRIVATE key.
//
// A public and a private ed25519 key are the same shape on disk — 64 hex characters — and
// `plankton keyid` accepts either without complaint. So `keys/session-1.key` where
// `registry/keys/session-1.pub` was meant is one character, produces no error anywhere, and the
// consequence is not a wrong answer but a disclosure: `cockpit show` and the published union build
// keys.json from exactly these entries, so the private key would be written into a committed file
// and served to every viewer.
//
// Two checks, because neither is sufficient alone. The extension is a floor that holds even for a
// key this config never names. Comparing against the identity keys is exact for the transposition
// that actually happens — swapping one of THIS repo's own halves — since the config names both.
func validateTrustTiers(raw *Raw) error {
	for tier, entries := range raw.Trust.Tiers {
		for _, entry := range entries {
			if strings.EqualFold(filepath.Ext(entry), ".key") {
				return fmt.Errorf(
					"trust.tiers[%q] names %s, which is a private key by its extension — trust tiers hold the "+
						"PUBLIC halves (.pub). keys.json is built from these and committed, so this would "+
						"publish a signing key", tier, entry)
			}
		}
	}
	return nil
}

// checkTrustTierContents is the exact half of the same check, and it lives here rather than in
// validate because it has to read files: the entries are repo-relative, and resolving them against
// the process's working directory instead of the repo root would make it read nothing and pass
// everything — a check that cannot fire.
func checkTrustTierContents(root string, raw *Raw) error {
	private := map[string]string{}
	for _, p := range []string{raw.Identity.PlanktonKey, raw.Identity.NektonKey} {
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(root, p)); err == nil {
			private[strings.TrimSpace(string(b))] = p
		}
	}
	if len(private) == 0 {
		return nil
	}
	for tier, entries := range raw.Trust.Tiers {
		for _, entry := range entries {
			path := entry
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, entry)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue // a missing path is reported later, by whatever tries to read it
			}
			if named, isPrivate := private[strings.TrimSpace(string(b))]; isPrivate {
				return fmt.Errorf(
					"trust.tiers[%q] names %s, whose contents are this repo's own signing key (%s) — trust "+
						"tiers hold public halves, and keys.json is built from them and committed",
					tier, entry, named)
			}
		}
	}
	return nil
}

// ValidateEnvRef checks an exact execution-environment reference, wherever it came from: the
// config, or the caller of cockpit_publish. The substrate does not interpret the value — it may be
// a container image, a nix store path, a run-server id — so the only thing that can be checked is
// the one form that is checkable, and the one that has a common way of being wrong.
func ValidateEnvRef(ref string) error {
	// A tag is a moving target: `oci://img:latest` names whatever that tag points at today, so a
	// reproduction committing to "this environment" would commit to nothing. Only a digest pins.
	if strings.HasPrefix(ref, "oci://") && !strings.Contains(ref, "@sha256:") {
		return fmt.Errorf(
			"envRef %q is an OCI reference without a digest — a tag names whatever it points at "+
				"today, so it pins no environment at all. Use oci://<image>@sha256:<digest>", ref)
	}
	return nil
}

// validateEnvironment rejects the two ways an environment pin can be quietly meaningless. Both
// values are COVERED, so a wrong one does not fail — it silently produces a foton that pins
// something other than what ran, and no later check can tell.
func validateEnvironment(env Environment) error {
	if env.Spectrum != "" && !strings.HasPrefix(env.Spectrum, "sha256:") {
		return fmt.Errorf("environment.spectrum must be an env-spectrum content hash (sha256:...), got %q", env.Spectrum)
	}
	return ValidateEnvRef(env.EnvRef)
}

// AllowsTemplate reports whether template is in the configured claim-template ceiling.
func (c *Config) AllowsTemplate(template string) bool {
	for _, t := range c.Raw.Claims.AllowedTemplates {
		if t == template {
			return true
		}
	}
	return false
}

// TierPubkeys returns the absolute paths of every pubkey configured across all trust tiers,
// paired with the tier name each belongs to. Used by the verify package to resolve a record's
// tier from its actual verifying key, never from a declared keyid.
func (c *Config) TierPubkeys() map[string]string {
	out := map[string]string{}
	for tier, keys := range c.Raw.Trust.Tiers {
		for _, k := range keys {
			p := k
			if !filepath.IsAbs(p) {
				p = filepath.Join(c.RepoRoot, p)
			}
			out[p] = tier
		}
	}
	return out
}

func repoRootOf(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var remoteRe = regexp.MustCompile(`(?:github\.com[:/])([^/]+)/([^/.]+?)(?:\.git)?$`)

// originURL reads a repository's origin remote, or errors if it has none.
func originURL(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkRemoteMatches is the hard-refuse step of the anti-wrong-folder guard: the repo's own
// `origin` remote must name the exact owner/repo the config claims to be. On any mismatch —
// including no remote at all — every cockpit tool call refuses outright.
func checkRemoteMatches(ctx context.Context, repoRoot string, want RepoRef) error {
	url, err := originURL(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("could not read git remote 'origin' in %s: %w", repoRoot, err)
	}
	m := remoteRe.FindStringSubmatch(url)
	if m == nil {
		return fmt.Errorf("remote 'origin' (%s) is not a recognizable github.com owner/repo URL", url)
	}
	owner, name := m[1], m[2]
	if !strings.EqualFold(owner, want.Owner) || !strings.EqualFold(name, want.Name) {
		return fmt.Errorf(
			"repo/remote mismatch: cockpit.config.json says %s/%s, but this repo's origin is %s/%s — refusing to act against the wrong repo",
			want.Owner, want.Name, owner, name,
		)
	}
	return nil
}

// validateMaterial checks the shape of every attachment, and nothing about its contents.
//
// The line it holds is worth stating, because it is the whole point of this block: it refuses
// entries that are unreadable or dangerous, NEVER entries whose scheme it has not heard of. An
// unlisted scheme is a supported case, not an error — the kernel takes it, and so does this.
func validateMaterial(m Material) error {
	seen := map[string]bool{}
	for i, a := range m.Attach {
		where := fmt.Sprintf("material.attach[%d]", i)
		if a.Scheme == "" {
			return fmt.Errorf("%s has no scheme — evidence must say what produced it, even when nothing here can read it", where)
		}
		if strings.ContainsAny(a.Scheme, " \t\"") {
			return fmt.Errorf("%s scheme %q contains whitespace or a quote; it is passed to `plankton attach --scheme` as one token", where, a.Scheme)
		}
		if a.MediaType == "" {
			return fmt.Errorf(
				"%s (scheme %q) has no mediaType — it is required here even for a scheme the kernel has a "+
					"default for, so this repo's records do not change what they carry because a default "+
					"table in the kernel changed", where, a.Scheme)
		}
		if a.File == "" {
			return fmt.Errorf("%s (scheme %q) names no file", where, a.Scheme)
		}
		if filepath.IsAbs(a.File) || !filepath.IsLocal(a.File) {
			return fmt.Errorf(
				"%s file %q must be a repo-relative path inside the repository — evidence is committed with "+
					"the record it is about, so a path outside it would attach bytes nobody receiving the "+
					"record can see", where, a.File)
		}
		if strings.HasSuffix(a.File, ".key") {
			return fmt.Errorf(
				"%s file %q matches *.key — verification material is evidence ABOUT a record and is stored "+
					"and shared with it; a private key must never be attached", where, a.File)
		}
		// A scheme+file pair attached twice would append the same bytes to the record twice. The
		// kernel tolerates it (material is append-only and never deduplicated on write), which is
		// precisely why it is worth catching here instead of leaving a doubled entry in every record.
		k := a.Scheme + "\x00" + a.File
		if seen[k] {
			return fmt.Errorf("%s repeats scheme %q with file %q — it would be attached to every record twice", where, a.Scheme, a.File)
		}
		seen[k] = true
	}
	for i, r := range m.X509Roots {
		if r == "" {
			return fmt.Errorf("material.x509Roots[%d] is empty", i)
		}
		if strings.HasSuffix(r, ".key") {
			return fmt.Errorf("material.x509Roots[%d] (%s) matches *.key — a trust anchor is a certificate, not a private key", i, r)
		}
	}
	return nil
}

// checkMaterialFiles is the half of the check that needs the repository root: the bytes must
// actually be there, and a configured trust anchor must actually parse.
//
// Both are checked at load rather than at publish. A missing certificate discovered mid-publish
// would leave a record authored and its evidence absent, and a root that turns out not to be a
// certificate would silently mean every attachment reports CARRIED forever — a verification that
// never happens, with nothing saying so.
func checkMaterialFiles(root string, m Material) error {
	for i, a := range m.Attach {
		p := filepath.Join(root, a.File)
		if st, err := os.Stat(p); err != nil {
			return fmt.Errorf("material.attach[%d] (scheme %q) names %s, which cannot be read: %w", i, a.Scheme, a.File, err)
		} else if st.IsDir() {
			return fmt.Errorf("material.attach[%d] (scheme %q) names %s, which is a directory", i, a.Scheme, a.File)
		}
	}
	for i, r := range m.X509Roots {
		p := r
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("material.x509Roots[%d] (%s) cannot be read: %w", i, r, err)
		}
		if !x509.NewCertPool().AppendCertsFromPEM(b) {
			return fmt.Errorf(
				"material.x509Roots[%d] (%s) contains no PEM certificate — with no usable root every "+
					"attached certificate would be reported as carried-but-unverified forever, and nothing "+
					"would say why", i, r)
		}
	}
	return nil
}

// ResolvedAttachment is one configured attachment with its path made absolute.
type ResolvedAttachment struct {
	Scheme    string
	MediaType string
	File      string // absolute
}

// Attachments returns every configured attachment with an absolute file path, in config order.
func (c *Config) Attachments() []ResolvedAttachment {
	out := make([]ResolvedAttachment, 0, len(c.Raw.Material.Attach))
	for _, a := range c.Raw.Material.Attach {
		out = append(out, ResolvedAttachment{
			Scheme: a.Scheme, MediaType: a.MediaType,
			File: filepath.Join(c.RepoRoot, a.File),
		})
	}
	return out
}

// X509Roots returns the configured trust anchors as one pool, or nil when none are configured.
//
// nil is meaningful and must not be replaced by the system pool: a caller that gets nil reports
// CARRIED, which is the honest answer when this repo has declared no root to judge against.
func (c *Config) X509Roots() *x509.CertPool {
	if len(c.Raw.Material.X509Roots) == 0 {
		return nil
	}
	pool := x509.NewCertPool()
	for _, r := range c.Raw.Material.X509Roots {
		p := r
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.RepoRoot, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue // unreadable here means it was readable at load and vanished since; CARRIED is then correct
		}
		pool.AppendCertsFromPEM(b)
	}
	return pool
}
