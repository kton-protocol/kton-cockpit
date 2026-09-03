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
// and the proof is stored beside the record as verification material (SPEC §8.1).
//
// Off by default, and that default is not timidity. Anchoring writes to a PUBLIC, PERMANENT log:
// the entry cannot be withdrawn, and while the payload is a signature over a hash rather than the
// data, the fact that this repo produced a record at that moment becomes public and stays public.
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
	RequiredLevel string `json:"requiredLevel"`
	Normalizer    string `json:"normalizer"`
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
	if err := validateEnvironment(raw.Environment); err != nil {
		return err
	}
	if err := validateExecution(raw.Execution, raw.Environment); err != nil {
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
