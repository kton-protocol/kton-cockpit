package cli

// uilist.go is `cockpit ui list`: what the installed packages declare for a frontend, for a shell to
// read instead of package folders (userinterface's QM Workbench, contract C2.4; serve's ui.list in
// v0.6.0). The cockpit parses no package format itself: what a package declares comes from ktonpkg
// (v0.9.0: UIEntries, Blocks and Compositions; its Verify includes a verticle's ui/ problems, which
// are listed under refused with the file they name).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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
	// UI, Blocks and Compositions are what the package declares, as ktonpkg reads them: each entry
	// is ktonpkg's own object plus its file name (file for ui/, name for blocks and compositions).
	// An empty list when it declares none; null when the package is not admitted, fails its check
	// or could not be read (see allowed and refused): a frontend uses only what is listed.
	UI           []map[string]any `json:"ui"`
	Blocks       []map[string]any `json:"blocks"`
	Compositions []map[string]any `json:"compositions"`
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
		r := e.Revision
		var errs []error
		ui, err := r.UIEntries()
		errs = append(errs, err)
		p.UI = declared(ui, "file", err)
		bs, err := r.Blocks()
		errs = append(errs, err)
		p.Blocks = declared(bs, "name", err)
		cs, err := r.Compositions()
		errs = append(errs, err)
		p.Compositions = declared(cs, "name", err)
		for _, err := range errs {
			if err != nil {
				p.Refused = append(p.Refused, UIRefusal{Code: "ui.check", Reason: err.Error()})
			}
		}
		// Declarations are for a frontend to use, so they are listed only for a package it may use:
		// admitted and passing its check. Otherwise they are null, and refused or allowed says why
		// (kton-review V1).
		if !p.Allowed || len(p.Refused) > 0 {
			p.UI, p.Blocks, p.Compositions = nil, nil, nil
		}
		out.Packages = append(out.Packages, p)
	}
	return out, nil
}

// declared is ktonpkg's decoded declarations as a list sorted by name, each with its name under key;
// nil when they could not be read, so the field is absent rather than empty.
func declared[T any](m map[string]T, key string, err error) []map[string]any {
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []map[string]any{}
	for _, n := range names {
		b, _ := json.Marshal(m[n])
		var o map[string]any
		if json.Unmarshal(b, &o) != nil || o == nil {
			o = map[string]any{}
		}
		o[key] = n
		out = append(out, o)
	}
	return out
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
