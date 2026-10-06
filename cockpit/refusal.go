package cockpit

import "fmt"

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
	return &Refusal{Code: code, Clause: clause, Reason: fmt.Sprintf(format, args...)}
}
