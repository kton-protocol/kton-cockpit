package packages

import (
	"fmt"

	"github.com/gitmick/ktonpkg/scope"
)

// ForeignControlProblems checks every act of rev that says another package's control (ktonpkg
// v0.9.13) against the owner as installed here: the installation of that name at the pinned revision,
// its seal naming the pinned head. The build checks only the form; this is the rest, and it fails
// closed: an owner not installed is a problem, as is one installed only at another revision (which
// the problem then names) or more than once at the pinned one. With admitted given, an owner installed
// but not admitted is a problem too: the act signs with the owner's template, which say takes only
// from an admitted package.
func ForeignControlProblems(rev *scope.Revision, installed []Installed, admitted map[string]bool) []string {
	fcs, err := rev.ForeignControls()
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for _, fc := range fcs {
		var pinned, other []Installed
		for _, in := range installed {
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
			probs := scope.ForeignControlProblems(fc, pinned[0].Revision, pinned[0].Seal)
			if len(probs) == 0 && admitted != nil && !admitted[pinned[0].Package.ID] {
				probs = []string{fmt.Sprintf("%s: %s@%s is installed but not admitted (claims.allowedPackages), so its control cannot be signed",
					fc.Where, fc.From.Package, fc.From.At)}
			}
			out = append(out, probs...)
		case len(other) > 0:
			// Not at the pinned revision: the check names the revision that is installed.
			out = append(out, scope.ForeignControlProblems(fc, other[len(other)-1].Revision, other[len(other)-1].Seal)...)
		default:
			out = append(out, scope.ForeignControlProblems(fc, nil, scope.ClaimInfo{})...)
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
