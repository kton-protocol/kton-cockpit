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
	"github.com/gitmick/ktonpkg/scope"
	"github.com/kton-protocol/kton-cockpit/cockpit"
	"os"
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
	// Members are the sealed revision's parts, each by role, name and content hash: what ui cat
	// serves. Null with the declarations, for a package a frontend may not use.
	Members []UIMember `json:"members"`
	// Source is where the revision was built from (ktonpkg Revision.Source): null for an older
	// revision or a folder outside git. Requires are the packages it declares it needs (requires.json),
	// References every place its compositions name another package. Listed under the same rule as the
	// declarations (architecture view G7): the view draws requires solid and references dashed.
	Source     *scope.Source          `json:"source"`
	Requires   []scope.PackageRequire `json:"requires"`
	References []UIReference          `json:"references"`
	Refused    []UIRefusal            `json:"refused"`
}

// UIReference is one place a package's compositions name another package (ktonpkg
// Revision.PackageReferences): where (the composition, then the part ids), what (block, query,
// composition, every, render, control), the name taken, and a control's pin (at, head).
type UIReference struct {
	Where   string `json:"where"`
	What    string `json:"what"`
	Name    string `json:"name"`
	Package string `json:"package"`
	At      string `json:"at,omitempty"`
	Head    string `json:"head,omitempty"`
}

// UIMember is one part of a package's sealed revision (ktonpkg Revision.Members).
type UIMember struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Hash string `json:"hash"`
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
	// The packages a frontend may use are the vocabulary's: admitted, the latest revision of each id,
	// one per name, passing its check; ui cat serves only those, so ui list lists members only for those.
	usable, unusable, err := packages.Usable(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return out, err
	}
	isUsable, whyNot := map[string]bool{}, map[string]string{}
	for _, in := range usable {
		isUsable[in.Dir] = true
	}
	for _, r := range unusable {
		if r.Dir != "" && whyNot[r.Dir] == "" {
			whyNot[r.Dir] = r.Reason
		}
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
		if p.Allowed && len(p.Refused) == 0 && !isUsable[e.Dir] {
			why := whyNot[e.Dir]
			if why == "" {
				why = "not usable"
			}
			p.Refused = append(p.Refused, UIRefusal{Code: "package.unusable", Reason: why})
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
		ms, err := r.Members()
		errs = append(errs, err)
		if err == nil {
			p.Members = []UIMember{}
			hashOf := map[string]string{}
			for _, m := range ms {
				p.Members = append(p.Members, UIMember{m.Role, m.Name, m.Hash})
				hashOf[m.Role+":"+m.Name] = m.Hash
			}
			// A ui entry is named by its path in the package, a composition carries its hash, so a
			// frontend fetches its body with ui cat (userinterface's PR 2).
			for _, u := range p.UI {
				u["file"] = "ui/" + u["file"].(string) + ".json"
			}
			for _, c := range p.Compositions {
				c["hash"] = hashOf[scope.RoleComposition+":"+c["name"].(string)]
			}
		}
		src, err := r.Source()
		errs = append(errs, err)
		p.Source = src
		reqs, err := r.PackageRequires()
		errs = append(errs, err)
		if err == nil {
			p.Requires = append([]scope.PackageRequire{}, reqs...)
		}
		refs, err := r.PackageReferences()
		errs = append(errs, err)
		if err == nil {
			p.References = []UIReference{}
			for _, x := range refs {
				p.References = append(p.References, UIReference{x.Where, x.What, x.Name, x.Package, x.At, x.Head})
			}
		}
		for _, err := range errs {
			if err != nil {
				p.Refused = append(p.Refused, UIRefusal{Code: "ui.check", Reason: err.Error()})
			}
		}
		// Declarations are for a frontend to use, so they are listed only for a package it may use:
		// admitted and passing its check. Otherwise they are null, and refused or allowed says why
		// (kton-review V1).
		if !p.Allowed || len(p.Refused) > 0 {
			p.UI, p.Blocks, p.Compositions, p.Members = nil, nil, nil, nil
			p.Source, p.Requires, p.References = nil, nil, nil
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
	if len(args) >= 3 && args[0] == "cat" {
		var hashes []string
		asJSON := false
		for _, a := range args[2:] {
			if a == "--json" {
				asJSON = true
				continue
			}
			hashes = append(hashes, a)
		}
		if len(hashes) == 0 || (len(hashes) > 1 && !asJSON) {
			return fmt.Errorf("usage: cockpit ui cat <package> <sha256:hash> | cockpit ui cat <package> <sha256:hash>... --json")
		}
		return uiCat(ctx, args[1], hashes, asJSON)
	}
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: cockpit ui list [--json] | cockpit ui cat <package> <sha256:hash>... [--json]")
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

// uiCat writes one member of a usable package's sealed revision to stdout, by its hash: the bytes a
// frontend needs (a composition body, a carried viewer's file) without reading package folders
// itself (contract C1.5). It looks only among the usable packages, as the vocabulary does (admitted,
// passing its check, the latest revision per package, one per name), so an unadmitted package or a
// second installed revision of the same name does not stand in the way, and an unusable one is
// refused explicitly (kton-review U3, T7 on #170). The package is named by its name, its dir or its
// id. The bytes come through ktonpkg's MemberBytes, which checks membership and refuses records.
// UICatMember is one member in ui cat --json: its hash and its bytes, base64 (standard encoding).
type UICatMember struct {
	Hash  string `json:"hash"`
	Bytes []byte `json:"bytes"`
}

// UICat is ui cat --json: the package the reference named, and the members asked for, in the order
// asked. Every hash is read before anything is written: one that is not a member refuses the call.
type UICat struct {
	Package string        `json:"package"`
	Name    string        `json:"name"`
	Dir     string        `json:"dir"`
	Members []UICatMember `json:"members"`
}

func uiCat(ctx context.Context, ref string, hashes []string, asJSON bool) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	out, err := uiCatOf(cfg, ref, hashes)
	if err != nil {
		return err
	}
	if !asJSON {
		_, err = os.Stdout.Write(out.Members[0].Bytes)
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

// uiCatOf is what `ui cat` serves, for the command line and the frontend protocol alike: the members
// asked, all of them or none.
func uiCatOf(cfg *config.Config, ref string, hashes []string) (UICat, error) {
	if len(hashes) == 0 {
		return UICat{}, &cockpit.Refusal{Code: "argument", Clause: "SPEC §6", Reason: "ui cat needs a package and at least one member hash"}
	}
	usable, _, err := packages.Usable(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return UICat{}, err
	}
	var pkg *packages.Installed
	for i := range usable {
		in := &usable[i]
		if in.Package.Name == ref || in.Dir == ref || in.Package.ID == ref {
			if pkg != nil {
				// A reference that names two usable packages (one's name another's dir or id) is
				// refused rather than resolved by order (kton-review's nit on #170).
				return UICat{}, &cockpit.Refusal{Code: "ui.package", Clause: "SPEC §6", Reason: fmt.Sprintf("%q names both %s and %s: name it by its dir or id", ref, pkg.Dir, in.Dir)}
			}
			pkg = in
		}
	}
	if pkg == nil {
		return UICat{}, &cockpit.Refusal{Code: "ui.package", Clause: "SPEC §6", Reason: fmt.Sprintf("%q is not a usable installed package (admitted and passing its check): ui list says why", ref)}
	}
	out := UICat{Package: pkg.Package.ID, Name: pkg.Package.Name, Dir: pkg.Dir, Members: []UICatMember{}}
	for _, hash := range hashes {
		b, err := pkg.Revision.MemberBytes(hash)
		if err != nil {
			return UICat{}, &cockpit.Refusal{Code: "ui.member", Clause: "SPEC §6", Reason: fmt.Sprintf("%s of %s: %v", hash, pkg.Package.Name, err)}
		}
		out.Members = append(out.Members, UICatMember{Hash: hash, Bytes: b})
	}
	return out, nil
}
