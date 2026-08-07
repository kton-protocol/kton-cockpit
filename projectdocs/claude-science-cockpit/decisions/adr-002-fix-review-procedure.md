---
title: "Standard procedure for fixing review findings: cold session + 3 focused agents"
type: "decision"
project: "claude-science-cockpit"
date: 2026-08-07
status: "accepted"
tags: [process, review, quality]
---

# ADR-002: Standard procedure for fixing review findings — cold session + 3 focused agents

## Status

Accepted

## Context

Working through Michael's 5-item blocking review (see
[`reviews/michael-review-2026-08.md`](../reviews/michael-review-2026-08.md)) one fix at a time
established a pattern: implement a fix, then have an independent cold session (a fresh
Claude Code conversation with no prior context on this project or this fix) review it adversarially
before committing. This caught real regressions the implementing session missed — most notably:

- Fix #1: the first pass silently dropped a legitimate stderr warning
  (`producer`/`uses`/`lineage`'s "this read is INCOMPLETE" notice) on success, a real regression
  the cold session caught by independently reading the reference source.
- Fix #3: the first pass's redaction logic over-redacted trusted content whenever a claim's
  content-addressed predicate happened to embed an unrelated, untrusted hash — a genuine bug the
  cold session found by reasoning about nekton's spec (SPEC §2.2, term references), not just by
  re-reading the diff.

Both catches happened because the reviewing session had no stake in the fix already "looking done"
and went back to primary sources (the actual kernel reference code, the actual spec) rather than
trusting the implementing session's own account of what it had verified.

A single general-purpose cold review, however, is one perspective doing three different kinds of
work at once (is this well-factored, is it actually correct, is it safe) — spreading its attention
across concerns that benefit from being looked at in isolation, by a reviewer whose only job is
that one thing.

## Decision

**Every fix to a review finding in this project follows this procedure, going forward:**

1. Implement the fix. Build (`go build ./...`), vet (`go vet ./...`), and test (`go test ./...`)
   locally; verify live against real data where practical (a real vendored binary, a real
   registry) rather than relying on synthetic data alone wherever that's feasible.
2. Stage the change, uncommitted.
3. Run an independent **cold session**: a fresh Claude Code conversation, given only a
   self-contained prompt describing the original finding and the diff under review — no access to
   the implementing session's reasoning, only the finding, the diff, and the actual repo/reference
   sources it can go read itself.
4. Inside that cold session, fan out to **3 agents in parallel, each with a single, narrow
   mandate** — not one generalist reviewer trying to hold all three concerns in mind at once:
   - **Duplication**: does this fix introduce logic that already exists elsewhere in the codebase,
     or that should be factored into a shared helper rather than reimplemented at this call site?
   - **Soundness**: does this fix address the actual root cause, or does it paper over/work around
     the underlying issue (e.g. special-casing the symptom, silently swallowing the failure mode
     instead of fixing what produces it, fixing it only for the one code path checked and not
     others that share the same bug)? This is the same lens that caught fix #2's "is this
     targeting a capability that doesn't exist upstream" and fix #3's "is this actually redacting,
     or just narrowly patching the one scenario in the finding."
   - **Security**: does this fix (or the code path it touches) introduce or leave in place a
     security-relevant issue — data exposure, injection, path traversal, privilege/trust bypass,
     credential handling — independent of whether it resolves the original finding?
   The cold session synthesizes all 3 agents' findings into one verdict rather than relaying three
   separate unreconciled reports.
5. The implementing session (or the next one) incorporates anything the cold session's agents
   found, re-verifies (build/vet/test, plus a regression test reproducing the exact bug where
   practical — see fix #3's `TestRedactExcluded_TrustedLineSurvivesEmbeddedUntrustedTermRef`), and
   only then is a commit message produced. The human operator commits — no session commits on its
   own behalf.

## Consequences

### Positive

- Each of the 3 agents has a single job, so its attention isn't split across unrelated concerns —
  matching why fix #1 and #3's catches worked: a reviewer specifically looking for "does this
  match the spec/reference source" found something a reviewer just re-reading the diff for
  general plausibility would likely have missed.
- The soundness agent specifically institutionalizes the check this project's own rules already
  demand ([`what-we-dont-build.md`](../phases/design/what-we-dont-build.md): no cockpit-side
  workarounds for kernel gaps) — every fix gets explicitly asked "is this a real fix or a
  workaround" rather than that question only coming up when a human happens to think to ask it.
- A dedicated security pass on every fix, not just the ones that look security-relevant at a
  glance — fix #5 (the publish path denylist) is obviously security-relevant, but fix #3's
  redaction logic turned out to be too, in a way that wasn't obvious until someone looked
  specifically through that lens.

### Negative

- More overhead per fix than a single review pass — 3 agents plus a synthesizing session, versus
  one reviewer. Judged worth it given the review is one-time-per-fix and the fixes are exactly the
  kind of security/trust-boundary code where a missed regression is costly (see: fix #3's bug
  would have shipped a redaction feature that silently destroyed trusted content).
- Requires the cold session to actually fan out to 3 real independent agents rather than one agent
  performing three checklist passes serially in the same context — the isolation between the three
  lenses is what makes this different from (and intended to be stronger than) a single reviewer
  working through a 3-item checklist.

## Alternatives Considered

- **Single generalist cold-session review (the pattern used for fixes #1-#4)** — simpler, lower
  overhead, and it already worked twice. Superseded because a single reviewer's attention is
  necessarily divided across duplication/soundness/security concerns at once; splitting them
  across 3 focused agents is expected to catch more without meaningfully increasing turnaround
  (the 3 agents run in parallel, not in sequence).
- **Skip the cold session, rely on the implementing session's own testing** — rejected outright:
  this is exactly the failure mode the cold-session pattern was adopted to catch (a session
  reviewing its own work misses what it was already confident about), demonstrated concretely by
  fix #1 and fix #3 both shipping a real bug past the implementing session's own build/vet/test
  pass.
