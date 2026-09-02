// Package config loads and validates cockpit.config.json — the static ceiling the cockpit
// applies on every call. It is never written by the cockpit itself; Claude never sees or edits it
// through any tool.
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

type RepoRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

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

	repoRoot, err := repoRootOf(ctx, startDir)
	if err != nil {
		return nil, fmt.Errorf("cockpit: not inside a git repository (cwd=%s): %w", startDir, err)
	}

	cfgPath := filepath.Join(repoRoot, "cockpit.config.json")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("cockpit: no cockpit.config.json at repo root %s: %w", repoRoot, err)
	}

	var raw Raw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is not valid JSON: %w", cfgPath, err)
	}
	if err := validate(&raw); err != nil {
		return nil, fmt.Errorf("cockpit: cockpit.config.json at %s is invalid: %w", cfgPath, err)
	}

	if err := checkRemoteMatches(ctx, repoRoot, raw.Repo); err != nil {
		return nil, fmt.Errorf("cockpit: refusing to act — %w", err)
	}

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

func validate(raw *Raw) error {
	var missing []string
	if raw.Repo.Owner == "" {
		missing = append(missing, "repo.owner")
	}
	if raw.Repo.Name == "" {
		missing = append(missing, "repo.name")
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
	return validateEnvironment(raw.Environment)
}

// validateEnvironment rejects the two ways an environment pin can be quietly meaningless. Both
// values are COVERED, so a wrong one does not fail — it silently produces a foton that pins
// something other than what ran, and no later check can tell.
func validateEnvironment(env Environment) error {
	if env.Spectrum != "" && !strings.HasPrefix(env.Spectrum, "sha256:") {
		return fmt.Errorf("environment.spectrum must be an env-spectrum content hash (sha256:...), got %q", env.Spectrum)
	}
	// A tag is a moving target: `oci://img:latest` names whatever that tag points at today, so a
	// reproduction committing to "this environment" would commit to nothing. Only a digest pins.
	if strings.HasPrefix(env.EnvRef, "oci://") && !strings.Contains(env.EnvRef, "@sha256:") {
		return fmt.Errorf(
			"environment.envRef %q is an OCI reference without a digest — a tag names whatever it points at "+
				"today, so it pins no environment at all. Use oci://<image>@sha256:<digest>", env.EnvRef)
	}
	return nil
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

// checkRemoteMatches is the hard-refuse step of the anti-wrong-folder guard: the repo's own
// `origin` remote must name the exact owner/repo the config claims to be. On any mismatch —
// including no remote at all — every cockpit tool call refuses outright.
func checkRemoteMatches(ctx context.Context, repoRoot string, want RepoRef) error {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not read git remote 'origin' in %s: %w", repoRoot, err)
	}
	url := strings.TrimSpace(string(out))
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
