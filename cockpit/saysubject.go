package cockpit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/repofile"
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
	h, rel, err := repofile.Hash(cfg.RepoRoot, subject, true)
	switch {
	case errors.Is(err, repofile.ErrNotFound):
		return "", "", fmt.Sprintf("subject %q is not a sha256: id, an IRI, or a file in this repository", subject)
	case err != nil && strings.Contains(err.Error(), "is a directory"):
		return "", "", fmt.Sprintf("subject %q is a directory; for a run folder say about its record (cockpit run list --json gives it)", subject)
	case err != nil:
		return "", "", fmt.Sprintf("subject: %v", err)
	}
	return h, rel, ""
}
