package cli

// uilist.go is `cockpit ui list`: what the installed packages declare for a frontend, for a shell to
// read instead of package folders (userinterface's QM Workbench, contract C2.4; serve's ui.list in
// v0.6.0). The cockpit parses no package format itself: what a package declares comes from ktonpkg.
// Its ui/, blocks and compositions are not in any ktonpkg yet, so those fields are absent until
// ktonpkg reads them, which a shell can tell from "declares none" (an empty list).

import (
	"context"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// UIPackage is one installed package in `ui list --json`.
type UIPackage struct {
	Name     string `json:"name,omitempty"`
	Package  string `json:"package,omitempty"`  // the package id
	Revision string `json:"revision,omitempty"` // the sealed revision's id
	Label    string `json:"label,omitempty"`
	Profile  string `json:"profile,omitempty"`
	Dir      string `json:"dir"`
	Allowed  bool   `json:"allowed"`
	// UI, Blocks and Compositions are filled from ktonpkg once it reads them; absent until then.
	UI           []map[string]any `json:"ui,omitempty"`
	Blocks       []map[string]any `json:"blocks,omitempty"`
	Compositions []map[string]any `json:"compositions,omitempty"`
	Refused      []UIRefusal      `json:"refused"`
}

// UIRefusal is one part of a package that fails its check: file "" for the package as a whole.
type UIRefusal struct {
	File   string `json:"file"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// UIListing is `ui list --json`.
type UIListing struct {
	Packages []UIPackage `json:"packages"`
}

func uiListing(cfg *config.Config) (UIListing, error) {
	out := UIListing{Packages: []UIPackage{}}
	entries, err := packages.ListEach(cfg.RepoRoot)
	if err != nil {
		return out, err
	}
	admitted := map[string]bool{}
	for _, id := range cfg.Raw.Claims.AllowedPackages {
		admitted[id] = true
	}
	for _, e := range entries {
		p := UIPackage{Dir: e.Dir, Refused: []UIRefusal{}}
		if e.Err != nil {
			p.Refused = append(p.Refused, UIRefusal{Code: "package.check", Reason: e.Err.Error()})
			out.Packages = append(out.Packages, p)
			continue
		}
		p.Name, p.Package, p.Profile = e.Package.Name, e.Package.ID, e.Profile
		p.Revision, p.Label = e.Revision.ID, e.Revision.Label
		p.Allowed = admitted[e.Package.ID]
		for _, why := range e.Package.Verify() {
			p.Refused = append(p.Refused, UIRefusal{Code: "package.check", Reason: why})
		}
		out.Packages = append(out.Packages, p)
	}
	return out, nil
}

func runUI(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: cockpit ui list [--json]")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	l, err := uiListing(cfg)
	if err != nil {
		return err
	}
	if hasFlag(args[1:], "--json") {
		return printJSON(l)
	}
	if len(l.Packages) == 0 {
		fmt.Println("no packages installed (cockpit install <dir>)")
		return nil
	}
	for _, p := range l.Packages {
		state := "installed"
		if p.Allowed {
			state = "allowed"
		}
		if len(p.Refused) > 0 {
			state = "fails its check: " + p.Refused[0].Reason
		}
		fmt.Printf("%-24s %-10s %-12s %s\n", orDefault(p.Name, p.Dir), p.Label, p.Profile, state)
	}
	return nil
}
