package cockpit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit/module"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// Installing is where the two nektons meet. The package is its author's nekton; what this
// repository does with it is written into this repository's own. Install keeps the package whole
// under packages/<name>@<revision>/ (so it stays checkable as received), takes its records into
// this repository's registries (federation: union by hash, nothing rewritten), opens this
// repository's scope for it — named <name>@<revision>, the seed carries only the name — and says,
// in that scope, what was installed: the revision and the seal that fixes its state.

// InstallRequest names the package to install: a directory in the scope format.
type InstallRequest struct {
	Path string `json:"path" jsonschema:"the package directory to install (scope format), absolute or relative to the repository"`
	// Allow also admits the package in this repository's configuration (claims.allowedPackages), so
	// its templates and queries are in use. The operator's decision, made explicitly.
	Allow bool `json:"allow,omitempty" jsonschema:"also add the package to claims.allowedPackages, so its templates and queries are used"`
	// Via is the module installing the package (ADR-015 section 1). A package whose profile a linked
	// module claims is that module's to install: plain install refuses it and names the command.
	Via string `json:"-"`
}

// InstallResult reports an installation.
type InstallResult struct {
	Package  string `json:"package"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Revision string `json:"revision"`
	Profile  string `json:"profile,omitempty"`
	Seal     string `json:"seal"`
	Head     string `json:"head"`
	Ray      string `json:"ray,omitempty"`
	Dir      string `json:"dir"`
	Scope    string `json:"scope"`
	// ClaimID is the `installed` claim. Empty when no allowed package brings that template yet —
	// then Note says what to allow; the package is installed regardless.
	ClaimID string `json:"claimId,omitempty"`
	// AlreadyInstalled means this exact package was installed before; nothing was copied again.
	AlreadyInstalled bool `json:"alreadyInstalled,omitempty"`
	// Allowed reports whether claims.allowedPackages names this package, so its templates and
	// queries are in use here.
	Allowed bool `json:"allowed"`
	// AllowedNow means this call added the package to claims.allowedPackages (--allow).
	AllowedNow bool   `json:"allowedNow,omitempty"`
	Note       string `json:"note,omitempty"`
}

const installedTemplate = "installed"

// Install installs a package.
func (c *Cockpit) Install(ctx context.Context, in InstallRequest) (*InstallResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	// An install writes the package folder, the configuration, the package's records, its scope's seed
	// and the claim that it was installed. It holds the claims lock throughout, so two installs at once
	// neither race on the folder nor seed two scopes of one name nor chain onto one head, and reads the
	// configuration again under it, so neither loses the other's admission (kton-review M2 on #183).
	unlockClaims, lerr := binaries.New(cfg).LockClaims()
	if lerr != nil {
		return nil, refuse("store", "", "%v", lerr)
	}
	defer unlockClaims()
	if cfg, err = config.Load(ctx, c.dir()); err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if in.Path == "" {
		return nil, refuse("argument", "", "install needs the package directory")
	}
	src := in.Path
	if !filepath.IsAbs(src) {
		src = filepath.Join(cfg.RepoRoot, src)
	}
	p, err := scope.Open(src)
	if err != nil {
		return nil, refuse("package.unreadable", "", "%s is not a package in the scope format: %v", in.Path, err)
	}
	if probs := p.Verify(); len(probs) > 0 {
		return nil, refuse("package.invalid", "", "the package fails its structural check (%d finding(s)), first: %s", len(probs), probs[0])
	}
	rev, seal, err := p.Sealed()
	if err != nil {
		return nil, refuse("package.unsealed", "", "%v — only a sealed revision can be installed, since nothing else fixes what was installed", err)
	}
	// An act on another package's control is checked against the owner installed here (ktonpkg v0.9.13):
	// the owner is installed first, at the revision the act pins.
	if installed, err := packages.Installations(cfg.RepoRoot); err != nil {
		return nil, refuse("io", "", "reading the installed packages: %v", err)
	} else if probs := packages.ForeignControlProblems(rev, installed, nil); len(probs) > 0 {
		return nil, refuse("package.foreign", "", "%s acts on another package's control that is not installed as it pins (%d finding(s)), first: %s",
			p.Name, len(probs), probs[0])
	}
	profile, _ := rev.Profile()
	if m, ok := module.ForProfile(profile); ok && in.Via != m.Name {
		return nil, refuse("package.module", "", "%s is a %s package, which the %s module installs: cockpit %s install %s",
			p.Name, strings.TrimPrefix(profile, "https://kton.dev/profile/"), m.Name, m.Name, in.Path)
	} else if !ok && in.Via != "" {
		return nil, refuse("package.module", "", "the %s module does not install %s packages; use cockpit install",
			in.Via, strings.TrimPrefix(profile, "https://kton.dev/profile/"))
	}
	out := &InstallResult{Package: p.ID, Name: p.Name, Label: rev.Label, Revision: rev.ID, Profile: profile,
		Seal: seal.ID, Head: fmt.Sprint(seal.Object["hash"])}
	out.Ray, _ = rev.RayID()
	out.Dir = filepath.ToSlash(filepath.Join(packages.Dir, p.Name+"@"+rev.Label))
	for _, id := range cfg.Raw.Claims.AllowedPackages {
		out.Allowed = out.Allowed || id == p.ID
	}

	dst := filepath.Join(cfg.RepoRoot, filepath.FromSlash(out.Dir))
	if have, err := scope.Open(dst); err == nil {
		if have.ID != p.ID {
			return nil, refuse("package.conflict", "", "%s is already taken by a different package (%s); this one is %s",
				out.Dir, have.ID, p.ID)
		}
		out.AlreadyInstalled = true
	} else if _, statErr := os.Stat(dst); statErr == nil {
		return nil, refuse("package.conflict", "", "%s exists and is not a package", out.Dir)
	} else if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return nil, refuse("io", "", "copying the package: %v", err)
	}

	if in.Allow && !out.Allowed {
		if _, err := config.AllowPackage(cfg.RepoRoot, p.ID); err != nil {
			return nil, refuse("config", "", "adding %s to claims.allowedPackages: %v", p.ID, err)
		}
		cfg.Raw.Claims.AllowedPackages = append(cfg.Raw.Claims.AllowedPackages, p.ID)
		out.Allowed, out.AllowedNow = true, true
	}

	// The records go into this repository's registries from the copy, which is what stays here.
	kept, err := scope.Open(dst)
	if err != nil {
		return nil, refuse("io", "", "%v", err)
	}
	r := binaries.New(cfg)
	if _, err := kept.Install(rev.Label, cfg.PlanktonDir, cfg.NektonDir); err != nil {
		return nil, refuse("kernel", "", "taking the package's records into this registry: %v", err)
	}

	scopeName := p.Name + "@" + rev.Label
	scopeID, err := r.OwnScope(ctx, scopeName, cfg.NektonKey)
	if err != nil {
		return nil, refuse("scope.ambiguous", "", "%v", err)
	}
	if scopeID == "" {
		if scopeID, err = r.SeedScope(ctx, scopeName, cfg.NektonKey, ""); err != nil {
			return nil, refuse("kernel", "", "opening the scope %s: %v", scopeName, err)
		}
	}
	out.Scope = scopeID

	recorded := ""
	if chain, err := r.ScopeChain(ctx, scopeID); err == nil {
		for _, cl := range chain {
			if cl.Subject == out.Revision && strings.HasSuffix(cl.Predicate, "/"+installedTemplate) {
				recorded = cl.ID
			}
		}
	}
	if recorded != "" {
		out.ClaimID = recorded // installed before and said so; saying it again would only repeat it
	} else if allowsTemplate(cfg, installedTemplate) {
		said, err := c.sayIn(ctx, cfg, r, out.Revision, installedTemplate, map[string]string{
			"package": out.Package, "seal": out.Seal, "head": out.Head, "ray": rayOrPackage(out),
			"name": out.Name, "label": out.Label,
		}, scopeName)
		if err != nil {
			return nil, err
		}
		out.ClaimID = said
	} else {
		out.Note = fmt.Sprintf("installed. No %q claim was written, which is fine: no allowed package brings that template. "+
			"Allow a vocabulary package that has it, and later installs are also recorded as claims", installedTemplate)
	}
	if !out.Allowed && out.Profile == scope.ProfileTemplates {
		out.Note = fmt.Sprintf("a vocabulary package: its templates and queries are used only once it is allowed — "+
			"run install again with --allow, or add %s to claims.allowedPackages", out.Package)
	}

	paths := []string{out.Dir, config.Rel("plankton"), config.Rel("nekton")}
	if out.AllowedNow {
		paths = append(paths, config.Rel(config.ConfigFile))
	}
	if _, err := persist(ctx, cfg, paths,
		"install: "+scopeName); err != nil {
		return nil, refuse(storeCode(err), "", "committing the installation failed: %v", err)
	}
	return out, nil
}

// rayOrPackage is what the installed claim's ray field names: the ray of a workflow, and for a
// package without one the package itself.
func rayOrPackage(in *InstallResult) string {
	if in.Ray != "" {
		return in.Ray
	}
	return in.Package
}

// sayIn writes a claim from an allowed template into a scope of this repository, through the same
// path say takes for its chain. The caller holds the claims lock (binaries.Runner.LockClaims).
func (c *Cockpit) sayIn(ctx context.Context, cfg *config.Config, r *binaries.Runner, subject, template string,
	fields map[string]string, scopeName string) (string, error) {
	chain, _, cerr := resolveChain(ctx, r, cfg, scopeName)
	if cerr != "" {
		return "", refuse("scope.unknown", "", "%s", cerr)
	}
	id, err := r.Annotate(ctx, subject, template, fields, cfg.NektonKey, chain)
	if err != nil {
		return "", refuse("kernel", "", "nekton annotate failed: %v", err)
	}
	return id, nil
}

// FormatInstall is the human-readable report of an installation, the same for `cockpit install` and
// a module's install command.
func FormatInstall(out *InstallResult) string {
	short := func(id string) string {
		id = strings.TrimPrefix(id, "sha256:")
		if len(id) > 12 {
			return id[:12]
		}
		return id
	}
	var b strings.Builder
	state := "installed"
	if out.AlreadyInstalled {
		state = "already installed"
	}
	fmt.Fprintf(&b, "%s %s, revision %s  →  %s\n", state, out.Name, out.Label, out.Dir)
	fmt.Fprintf(&b, "  package   %s\n", out.Package)
	fmt.Fprintf(&b, "  profile   %s\n", strings.TrimPrefix(out.Profile, "https://kton.dev/profile/"))
	fmt.Fprintf(&b, "  sealed    %s (head %s)\n", short(out.Seal), short(out.Head))
	if out.Ray != "" {
		fmt.Fprintf(&b, "  ray       %s\n", out.Ray)
	}
	fmt.Fprintf(&b, "  scope     %s@%s (%s)\n", out.Name, out.Label, short(out.Scope))
	if out.ClaimID != "" {
		fmt.Fprintf(&b, "  recorded  installed, claim %s\n", short(out.ClaimID))
	}
	if strings.HasSuffix(out.Profile, "/templates") {
		allowed := "no — its templates and queries are not in use"
		if out.AllowedNow {
			allowed = "yes, added to claims.allowedPackages — its templates and queries are in use"
		} else if out.Allowed {
			allowed = "yes — its templates and queries are in use"
		}
		fmt.Fprintf(&b, "  allowed   %s\n", allowed)
	}
	if out.Note != "" {
		fmt.Fprintf(&b, "\n  note: %s\n", out.Note)
	}
	return b.String()
}
