package cockpit

import (
	"errors"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/gitops"
	"github.com/kton-protocol/kton-cockpit/internal/keyload"
)

// Refusal is the cockpit declining to act, with a reason. It is returned as an error.
//
// Code is stable and meant for programs: a caller in Go, R or TypeScript branches on it rather than
// on the wording of Reason, which is meant for people and may be improved. Clause names the part of
// the specification the refusal enforces, when there is one ("SPEC §7.3"); it is empty for a failure
// underneath the cockpit — a kernel call, a commit — that the cockpit reports rather than decides.
//
// The codes in use:
//
//	binding                   the repository is not the one this configuration belongs to (SPEC §5)
//	verb.disabled             the configuration turns this verb off (SPEC §6)
//	argument                  the request is incomplete, contradictory or names an unknown query
//	path.denied               publish would touch a path it never touches (SPEC §7.3)
//	env-ref                   the named execution environment is malformed or contradicts the run (SPEC §7.5)
//	execution.not-configured  the request needs a run this repository does not do (SPEC §10)
//	execution.failed          the command failed in its container (SPEC §10)
//	execution.no-output       the run did not produce what was declared (SPEC §10)
//	output-dir.not-empty      outputDir held files before the run (SPEC §10)
//	corpus.uncorroborated     a basis lacks the reproductions this repository requires (SPEC §8.4)
//	template.not-allowed      the claim template is outside the configured ceiling (SPEC §8.1)
//	reproduction.not-met      the reproduction precondition failed (SPEC §8.2)
//	scope.unknown             the claim scope is not configured or not ingested
//	filter.invalid            an ask filter names something that does not exist (SPEC §9.2)
//	scope                     a scope could not be read or judged (SPEC §9.6)
//	identity.locked           the signing key is encrypted and no passphrase was given, or it was wrong (ADR-021)
//	claim.not-registered      a signed claim did not appear when queried back
//	material, anchor, union   attaching evidence, anchoring or publishing the union failed
//	kernel, store, io         a kernel call, the backend's commit/push, or the file system failed
type Refusal struct {
	Code   string `json:"code"`
	Clause string `json:"clause,omitempty"`
	Reason string `json:"reason"`
}

// Error is the reason alone, exactly as a person reads it.
func (r *Refusal) Error() string { return r.Reason }

func refuse(code, clause, format string, args ...any) error {
	// A key that is locked is identity.locked wherever it surfaced, whatever the call that wanted it
	// meant to report (a kernel call, a scope's seed): the caller names no code for it, and the
	// program that reads codes needs one answer (ADR-021).
	for _, a := range args {
		if err, ok := a.(error); ok {
			if r := LockedRefusal(err); r != nil {
				return r
			}
		}
	}
	return &Refusal{Code: code, Clause: clause, Reason: fmt.Sprintf(format, args...)}
}

// LockedRefusal is the identity.locked refusal for an error that holds a locked key (an encrypted
// signing key without its passphrase, or with a wrong one), nil for any other error. The reason names
// the key file, its keyid and the ways to unlock it; it never holds a passphrase.
func LockedRefusal(err error) *Refusal {
	var l *keyload.LockedError
	if errors.As(err, &l) {
		return &Refusal{Code: "identity.locked", Clause: "ADR-021", Reason: l.Error()}
	}
	return nil
}

// unlockKeys opens the signing keys a call will need before it takes a lock, starts a child or writes
// anything: a person is asked once, at the start, and a missing or wrong passphrase refuses the call
// before any step has run (ADR-021 §4). The keys stay open in r for the rest of the call.
func unlockKeys(r *binaries.Runner, paths ...string) error { return UnlockKeys(r, paths...) }

// UnlockKeys is unlockKeys for the command line's own signers (a scope's seed and seal).
func UnlockKeys(r *binaries.Runner, paths ...string) error {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := r.SigningKey(p); err != nil {
			// Only a locked key is refused here. A key that is missing or malformed is reported by
			// the call that signs with it, as it always was.
			if lr := LockedRefusal(err); lr != nil {
				return lr
			}
		}
	}
	return nil
}

// storeCode is the refusal code for a failed write to the store: store.busy when another git held
// the repository's lock past the wait, so a caller can try again, else store (SPEC §12.1).
func storeCode(err error) string {
	if errors.Is(err, gitops.ErrBusy) {
		return "store.busy"
	}
	return "store"
}

// Pending is a write that is done but not yet committed: the record is signed and stored, and only
// the git commit met another git's lock past the wait. It is not refused, since the record exists
// and a retry would sign a second one (kton-review B1 on #152); the next write commits it with its own.
type Pending struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// pendingIfBusy turns a store.busy after signing into Pending; any other error stays an error.
func pendingIfBusy(err error, what string) (*Pending, error) {
	if err == nil || !errors.Is(err, gitops.ErrBusy) {
		return nil, err
	}
	return &Pending{Code: "store.busy", Reason: what + " is signed and stored, but not committed: another git held the repository's lock. The next write commits it; do not sign it again."}, nil
}
