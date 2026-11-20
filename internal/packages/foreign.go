package packages

import (
	"fmt"

	"github.com/gitmick/ktonpkg/scope"
)

// ForeignControlProblems checks every act of rev that says another package's control (ktonpkg
// v0.9.13) against its owner. The build checks only the form; this is the rest, and it fails closed.
// The owner is resolved among owners, the revisions that would actually be used: the served set for
// a frontend (one revision per package, the latest, admitted, passing its check), the latest installed
// revision of each package at install. It must be the pinned revision (label, and the head its seal
// names): a later revision installed beside the pinned one is not it, since say takes the template from
// the revision in use (kton-review F1 on #206). installed only explains an owner that is installed but
// not among owners.
func ForeignControlProblems(rev *scope.Revision, owners, installed []Installed) []string {
	fcs, err := rev.ForeignControls()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, fc := range fcs {
		var pinned, other []Installed
		for _, in := range owners {
			if in.Package.Name != fc.From.Package {
				continue
			}
			if in.Revision.Label == fc.From.At {
				pinned = append(pinned, in)
			} else {
				other = append(other, in)
			}
		}
		switch {
		case len(pinned) > 1:
			out = append(out, fmt.Sprintf("%s: %s@%s is installed more than once", fc.Where, fc.From.Package, fc.From.At))
		case len(pinned) == 1:
			out = append(out, scope.ForeignControlProblems(fc, pinned[0].Revision, pinned[0].Seal)...)
		case len(other) > 0:
			// Another revision is the one in use: the check names it.
			out = append(out, scope.ForeignControlProblems(fc, other[len(other)-1].Revision, other[len(other)-1].Seal)...)
		case installedAs(installed, fc.From.Package):
			out = append(out, fmt.Sprintf("%s: %s is installed but not in use (not admitted, or refused: ui list says why), so its control cannot be signed",
				fc.Where, fc.From.Package))
		default:
			out = append(out, scope.ForeignControlProblems(fc, nil, scope.ClaimInfo{})...)
		}
	}
	return out
}

func installedAs(installed []Installed, name string) bool {
	for _, in := range installed {
		if in.Package.Name == name {
			return true
		}
	}
	return false
}

// Latest are the installations no other installation of the same package follows: the revision of
// each package an act on its control would sign with, once admitted.
func Latest(installed []Installed) []Installed {
	var out []Installed
	for _, in := range installed {
		superseded := false
		for _, o := range installed {
			if o.Package.ID == in.Package.ID && o.Revision.ID != in.Revision.ID && contains(o, in.Revision.ID) {
				superseded = true
				break
			}
		}
		if !superseded {
			out = append(out, in)
		}
	}
	return out
}

// Installations are the readable installed packages.
func Installations(repoRoot string) ([]Installed, error) {
	entries, err := ListEach(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, e := range entries {
		if e.Err == nil {
			out = append(out, e.Installed)
		}
	}
	return out, nil
}
