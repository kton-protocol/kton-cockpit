package cockpit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
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

// IsIRI reports whether s names something by IRI as a say subject may (SPEC §8): a scheme with an
// authority, or urn:, tag:, did: or mailto:. The shell form uses it so every form takes the same.
func IsIRI(s string) bool { return iriSubject.MatchString(s) }

// isPartialID says v begins as a kton id (sha256:) and is not a whole one.
func isPartialID(v string) bool {
	if !strings.HasPrefix(strings.ToLower(v), "sha256:") {
		return false
	}
	_, ok := core.NormalizeContentHash(v)
	return !ok
}

// fullIDHint names the one record or claim a short id is the start of, so the refusal says which id to
// paste; "" when none or more than one is. A short id is never signed in its place: a prefix that is
// unique today need not be tomorrow. Only this repository's own registry is searched, as it is on disk
// (no source is fetched to word a refusal), and only what a trusted key signed is offered (kton-review
// N1/N2 on #230).
func fullIDHint(ctx context.Context, cfg *config.Config, v string) string {
	if !isPartialID(v) {
		return ""
	}
	prefix := strings.ToLower(strings.TrimRight(strings.TrimSpace(v), ".…"))
	hexPart := strings.TrimPrefix(prefix, "sha256:")
	if len(hexPart) < 6 || strings.Trim(hexPart, "0123456789abcdef") != "" {
		return ""
	}
	keys, err := binaries.TrustKeys(cfg, "")
	if err != nil {
		return ""
	}
	trusted := func(env core.Envelope) bool { return core.VerifiedSignerKeyID(env, keys) != "" }
	seen := map[string]bool{}
	if p, err := binaries.OpenPlanktonSource(cfg.PlanktonDir); err == nil {
		for _, r := range p.Records(0) {
			id := strings.ToLower(r.FotonID)
			if strings.HasPrefix(id, prefix) {
				if env, ok := p.Envelope(r.FotonID); ok && trusted(env) {
					seen[id] = true
				}
			}
		}
	}
	if n, err := binaries.OpenNektonSource(cfg.NektonDir); err == nil {
		for _, r := range n.RawRecords() {
			id := strings.ToLower(r.ClaimID)
			if strings.HasPrefix(id, prefix) && trusted(r.Envelope) {
				seen[id] = true
			}
		}
	}
	switch len(seen) {
	case 0:
		return "; nothing a trusted key signed in this repository's registry begins so (federation sources are not searched)"
	case 1:
		for id := range seen {
			return "; the one record or claim in this repository's registry that begins so is " + id + ": give it whole"
		}
	}
	return fmt.Sprintf("; %d records and claims in this repository's registry begin so: give the whole id", len(seen))
}
