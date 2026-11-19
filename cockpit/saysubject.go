package cockpit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// iriSubject is a subject that names something by IRI: a scheme with an authority, or one of the
// schemes a name stands on its own in.
var iriSubject = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*://\S+|(urn|tag|did|mailto):\S+)$`)

// resolveSubject is what a say request's subject names (SPEC §8): a record or claim id
// (sha256:<hex>, in lower case), an IRI as it is, or a file in this repository, which stands for its
// bytes and is reported back. Anything else is refused rather than signed as a literal: a claim about
// the text "data.txt" is about nothing ask can find (userinterface's v0.5.0 probe).
func resolveSubject(cfg *config.Config, subject string) (id, resolvedFrom, why string) {
	if strings.HasPrefix(subject, "sha256:") {
		n, ok := core.NormalizeContentHash(subject)
		if !ok {
			return "", "", fmt.Sprintf("subject %q is not a sha256: id (64 hex digits)", subject)
		}
		return n, "", ""
	}
	if iriSubject.MatchString(subject) {
		return subject, "", ""
	}
	abs := subject
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cfg.RepoRoot, filepath.FromSlash(subject))
	}
	rel, err := filepath.Rel(cfg.RepoRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Sprintf("subject %q is outside this repository; name a file in it, a sha256: id or an IRI", subject)
	}
	fi, err := os.Stat(abs)
	switch {
	case err != nil:
		return "", "", fmt.Sprintf("subject %q is not a sha256: id, an IRI, or a file in this repository", subject)
	case fi.IsDir():
		return "", "", fmt.Sprintf("subject %q is a directory; for a run folder say about its record (cockpit run list --json gives it)", subject)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", "", fmt.Sprintf("reading %s: %v", subject, err)
	}
	return core.HashBytes(b), filepath.ToSlash(rel), ""
}
