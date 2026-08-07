---
title: "Michael's cockpit review — fixes tracking"
project: "claude-science-cockpit"
status: "in-progress"
date: 2026-08-06
updated: 2026-08-06
---

# Michael's review — fixes tracking

Michael reviewed the cockpit and filed 5 blocking items, several "serious" items, and a handful of
smaller ones. Fixing the 5 blocking items one at a time, each as its own commit, each checked by an
independent cold-session review before commit. This doc tracks status and open TODOs across that
work; see the conversation/commit history for the full back-and-forth on each fix.

## Blocking items — status

| # | Finding | Status |
|---|---|---|
| 1 | `binaries.go` used `CombinedOutput()`, defeating `--print-id`'s stdout/stderr contract; `Author`'s fotonID came back as a 4-line blob | **Committed** (`7971ce4`). Cold-session review caught a regression in the first pass (stderr silently dropped even on success, losing `producer`/`uses`/`lineage`'s legitimate "this read is INCOMPLETE" warning) — fixed via a `planktonQuery` wrapper that folds success-path stderr back in for those three commands specifically. |
| 2 | `plankton reproductions` called without `--trust-keys` — self-declared, forgeable signer count | **Staged, not yet committed.** `Reproductions()` now materializes `cfg.TierPubkeys()` into a temp dir and passes `--trust-keys`. See "Open TODO" below — this fix is correct but currently makes `cockpit_ask query=reproductions` fail on every call, everywhere, because the capability doesn't exist upstream yet. |
| 3 | `ask.go`'s `Raw` field is not pre-filtered — every unverified record's text reaches the model anyway, despite `Included`/`Excluded` implying a real trust boundary | **Staged, not yet committed.** `Raw` is now redacted per-line before being returned. A cold-session review of the first pass found a real regression (see below) — fixed before commit. |
| 4 | `say.go` infers reproduction level (`L1` whenever `via != ""`) instead of parsing plankton's actual `reproduction: <level>` line — mislabels genuine L0 reproductions as L1 and then rejects them against an `L0` policy | **Staged, not yet committed.** Fixed by parsing `reproduction: <level>`. First fix reviewed under [ADR-002](../decisions/adr-002-fix-review-procedure.md)'s new 3-agent cold-session procedure — verdict: sound, no regressions, no security issues. One nit applied (tightened the regex's character class from `L[012]` to `L[01]`, since this command path never emits L2). |
| 5 | `cockpit_publish` has no denylist — Claude-supplied `outputs` could include e.g. `keys/session-1.key`, which the cockpit would commit, push, and build a public permalink to | Not started. |

## Open TODO: file an upstream plankton issue for `--trust-keys` on `reproductions`

While verifying fix #2, an independent cold-session review found that `--trust-keys` doesn't exist
on `plankton reproductions` **anywhere upstream**, in any revision (checked the full git history of
the local `/mnt/c/dev/plankton/plankton` checkout) — not just in the binaries vendored in
participant repos. Only `export --rdf` has it.

Digging further: this is not an undiscovered bug. Commit `e92beca` ("core+export: attribute only
cryptographically-verified signers (verified-attribution, RED)") already identified and fixed the
*exact same bug class* — every "derived surface" (its words) reading a DSSE envelope's
self-declared, unauthenticated `Signatures[0].KeyID` and presenting it as verified identity, when
`KeyID` is not itself cryptographically checked. That commit fixed it in exactly two places:
`plankton export --rdf --trust-keys <dir>` and `nekton export --nanopub --trust-keys <dir>`, via a
shared `core.VerifiedSignerKeyID(env, keys)` primitive. `reproductions` was never updated to use it
— it still reads `env.Signatures[0].KeyID` directly (see the `case "reproductions":` block in
`reference/cmd/plankton/main.go`), so it still has precisely the bug `e92beca` was written to
eliminate.

**TODO:** file an issue against `gitmick/plankton` requesting `--trust-keys` on `reproductions`,
framed as "extend the `e92beca` verified-attribution fix to this subcommand too" rather than a
generic feature request — the precedent (primitive, wiring pattern, and rationale) already exists
in that commit. A draft issue body exists in this conversation's history; not yet filed. Per this
project's own rule ("no own trust logic... raise a kernel-capability gap against the protocol spec,
don't build a cockpit-side workaround" — see
[`what-we-dont-build.md`](../phases/design/what-we-dont-build.md)), the cockpit should not attempt
to reconstruct this count itself (e.g. via `producer` + `verify` looped in Go) as a substitute.

**Until this lands upstream and a binary supporting it is vendored in:** `cockpit_ask
query=reproductions` will fail on every call, unconditionally — not just for unsigned/untrusted
data — because the vendored binary rejects the `--trust-keys` flag before it ever evaluates any
producer's signature. This is an intentional "fail loudly instead of serving a forgeable number"
tradeoff, not a bug in the fix itself, but it's a real, user-visible capability regression worth
knowing about before demoing this query.

## Fix #3 (Raw redaction) — a real bug found and fixed pre-commit

First pass: `redactExcluded` blanked any line of `Raw` containing *any* excluded record's id as a
plain substring, anywhere on the line. An independent cold-session review found this over-redacts:
nekton's claim predicate is spec-allowed to itself be a content hash (a "term reference", SPEC
§2.2), so a fully-verified, trusted claim's own `printClaims` line can legitimately contain a
*second*, unrelated `sha256:` hash — the predicate's term ref — which almost never resolves as a
real claim and so is almost always excluded in its own right. Substring matching wiped the entire
trusted claim's line because of that unrelated embedded hash — a genuine, self-inflicted cockpit
bug, not an upstream plankton/nekton issue (printing a content-addressed predicate inline is
expected, spec-compliant kernel behavior; nothing to raise against the protocol spec here).

Fixed by anchoring redaction to the line's own *first* `sha256:` match (verified against the
actual print statements for all six supported query types: that first match is always the
record's own id, never a secondary embedded reference) instead of any substring anywhere on the
line. Added `TestRedactExcluded_TrustedLineSurvivesEmbeddedUntrustedTermRef` as a direct regression
test, plus a sibling test confirming a record whose *own* leading id is excluded still gets
correctly redacted. Also folded in a cheap, non-blocking improvement the same review suggested:
the redaction placeholder now states the specific reason (unverified vs. verified-but-wrong-tier)
instead of one generic message.
