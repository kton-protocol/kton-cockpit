---
title: "Michael's cockpit review — fixes tracking"
project: "claude-science-cockpit"
status: "in-progress"
date: 2026-08-06
updated: 2026-08-12
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
| 5 | `cockpit_publish` has no denylist — Claude-supplied `outputs` could include e.g. `keys/session-1.key`, which the cockpit would commit, push, and build a public permalink to | **Staged, not yet committed.** Added `validatePublishPath` — see below for the critical bypass the cold-session review found and fixed before this closed. |

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

## Fix #5 (publish denylist) — a critical bypass found and fixed pre-commit

First pass implemented exactly what Michael asked: reject absolute/`..` paths, anything under
`keys_dir`, `.git`, `cockpit.config.json`, and `*.key` anywhere. The first 3-agent cold-session
review (ADR-002) found this was **not sufficient** — one blocking bypass, worse than the original
hole, plus two narrower gaps in the same fix:

- **Blocking, confirmed live**: `gitops.go`'s `git add` had no `--` separator
  (`args := append([]string{"add"}, paths...)`). `validatePublishPath` never rejected a path
  starting with `-`, so `outputs: ["-f", "."]` passed every check untouched, then became the
  literal command `git add -f .` — force-adding every gitignored file in the repo, real signing
  keys included, without the string `"keys/..."` ever appearing in the request. Strictly worse
  than Michael's original scenario: it doesn't require naming a key at all. Fixed at the root
  cause in `gitops.CommitAndPush` (`git add -- <paths>` — everything after `--` is unconditionally
  a literal pathspec to git, regardless of what it starts with), plus a defense-in-depth
  leading-dash rejection in `validatePublishPath` itself. Regression tests added at both layers:
  `TestCommitAndPush_AddInvocationUsesDashDashSeparator` (gitops) and
  `TestPublish_RefusesLeadingDashOutput` (integration, against the real repo, asserting nothing
  was even force-staged into the index — not just that HEAD didn't move).
- **`keys_dir` comparison used the unresolved raw config string**, not the already-resolved
  absolute `cfg.KeysDir`. If `keys_dir` is ever written anchored (e.g. `"/keys"` — nothing in
  `config.validate()` forbids this), `config.Load` still resolves it correctly, but the raw string
  compared as-is against a repo-relative path never shares a prefix, so the check silently never
  matches. Fixed by comparing against `cfg.KeysDir`.
- **Trailing dot/space defeated the `*.key` extension check** (`filepath.Ext("session-1.key.")`
  returns `"."`, not `".key"`) — directly contradicted that check's own "regardless of location"
  claim. Fixed with a `strings.TrimRight(clean, ". ")` before the extension check.

**Known, deliberately unfixed limitation** (documented in `validatePublishPath`'s own doc comment,
not silently assumed away): this is a path-only check. `cp keys/session-1.key data/results.csv`
then publishing `data/results.csv` sails through untouched — closing that needs content
inspection, a fundamentally different mechanism than a denylist. This fix closes the path-based
hole Michael described; content-based exfiltration is a separate, unaddressed risk.

## Open TODO: `in.Corpus` entries are never validated as real refs before being committed

Surfaced by the same cold-session review (Agent 3, security mandate) while checking fix #5 — not
one of Michael's original 5 items, a new finding. `PublishInput.Corpus` is documented as a list of
`sha256:...` nekton/plankton refs, written verbatim into a corpus manifest file that then gets
committed, pushed, and permalinked. Nothing checks that each entry actually looks like a real ref
before that happens — orthogonal to the path denylist (which only ever inspects *paths*, never
file *content*), so fix #5 correctly doesn't touch it, but it's an open gap worth a format check
(e.g. does each entry match `sha256:[0-9a-f]{64}`?) in a future pass, not addressed here.

## Findings 6-8 — status

Not part of Michael's original 5 blocking items or the corpus/trust-keys TODOs above; picked up
from a separate list of items surfaced during the same extended review effort (push explicitness,
push-on-nothing-staged, swallowed verify errors, plus "no binary capability/version pinning" and
"machine-bound tests", both still open — see below). Same procedure as fixes #1-5: implement,
build/vet/test, then an independent 3-agent cold-session review (ADR-002) before commit.

| # | Finding | Status |
|---|---|---|
| 6 | `gitops.go`'s `push()` called a bare `git push`, relying on ambient upstream-tracking/push.default state rather than being explicit | **Staged, not yet committed.** Fixed to `git push origin HEAD` in both the token and non-token auth branches — pushes exactly the checked-out commit to the same-named branch on origin regardless of local tracking config, and is a documented no-op when already in sync. Cold-session soundness review independently verified this is correct across `origin`-always-guaranteed (via `config.checkRemoteMatches`), detached-HEAD (fails cleanly, doesn't push somewhere unexpected), and non-default-branch scenarios. |
| 7 | `CommitAndPush`'s "nothing staged" early-return never attempted a push — a commit that succeeded on a prior call but whose push then failed (network blip, auth hiccup) would be stranded on the local clone forever, since every later publish of the same already-committed bytes takes this same branch | **Staged, not yet committed.** Fixed by attempting a push on that branch too. Cold-session soundness review then found a real trade-off in the first pass: pushing unconditionally there turns every republish of already-in-sync content into a real network round-trip (previously a pure local no-op) — reproduced this concretely hanging against an unreachable remote. Mitigated with a new `localAheadOfUpstream` check (compares HEAD against `@{u}`, purely local, no network call) that only pushes when local is actually ahead of or diverged from upstream, erring toward "push" whenever that comparison is ambiguous (e.g. no upstream tracking ref yet) so the original stranded-commit fix is never traded away. |
| 8 | `VerifyFoton`/`VerifyClaim` treated ANY non-zero exit from `plankton\|nekton verify` as "ok=false, err=nil" — a real error (e.g. a pubkey file deleted from a misconfigured trust tier) looked identical to a legitimate "wrong key" mismatch, with no error ever surfaced | **Staged, not yet committed.** Fixed with an `isVerifyMismatch(err)` helper: only `*exec.ExitError` with `ExitCode() == 2` (confirmed live against the real vendored binaries: exit 0 = valid, exit 2 = "UNVERIFIED - WRONG KEY", any other non-zero = a real error, prefixed `error: ...`) is treated as a non-error mismatch; everything else now propagates as a real Go error through `verify.ResolveTier` to `ask.go`'s caller, which correctly fails the whole query rather than silently excluding the record as "just untrusted." Cold-session soundness review independently re-ran the same three scenarios against real data in `participant-christian` and confirmed the 0/2/other split holds, and confirmed a missing binary (`*fs.PathError`, not an `ExitError`) correctly falls through to the real-error path too. |

**Critical bug found and fixed pre-commit, by the security-focused cold-session agent, while
verifying fix #6's redaction claim:** `push()`'s GITHUB_TOKEN/GH_TOKEN auth header redaction never
actually worked. `runRedacted`/`redactSlice` matched the raw token string, but the credential only
ever appears in argv base64-encoded (`AUTHORIZATION: basic <base64(x-access-token:<token>)>`) —
base64 does not preserve substrings, so the redaction pass matched nothing. The agent reproduced
it concretely: a failed push with `GITHUB_TOKEN` set returned the fully intact, trivially-decodable
base64 blob straight into the error text, which flows through `CommitAndPush` into
`internal/tools/publish.go`'s `errResult(...)` — directly into the MCP tool result shown to the
Claude session. This is exactly the sandboxed, less-trusted runtime (claude-science's Local command
connector) this token mechanism exists for, and finding 7 makes the vulnerable path fire on every
publish call, not only ones with a fresh commit — compounding the exposure. This bug predates this
diff (the header-building/redaction logic itself wasn't touched by fix #6), but the review brief
specifically asked to verify redaction against the new argv shape, which is what surfaced it.
Fixed by passing both the raw token AND the constructed header to `runRedacted`
(`redactSlice`/`runRedacted` now take `secrets ...string`, not a single `secret string`), so
either form gets scrubbed. Regression test `TestPush_RedactsBothRawTokenAndBase64EncodedHeaderOnFailure`
added — confirmed it fails against the pre-fix code (reproducing the exact leak) and passes
against the fix.

**Same bug class as pre-fix finding 8, found independently by both the duplication and soundness
agents, in the sibling function `Reproduces` (same file):** `Reproduces` still did the bare
`if err != nil { return false, out, nil }` collapse — worse than pre-fix `verify`, since
`reproduces` doesn't expose a distinguishing exit code between a genuine non-match and a usage
error (both return exit 1). The soundness agent confirmed via live testing that this also silently
swallowed exec-level failures (a missing/broken `plankton` binary produces `*fs.PathError`, treated
identically to "outputs don't match"), and confirmed the consequence: `say.go`'s
`determineReproductionLevel` has a real `if err != nil` branch after this call that was dead code —
unreachable given the pre-fix `Reproduces` could never return a non-nil error. Fixed to distinguish
an exec-level failure (not an `*exec.ExitError` — missing binary, permission denied) from the
binary actually running and reporting non-zero (still treated as a legitimate non-match, since
there's no reliable exit code to further discriminate a usage error from a real non-match here —
a known, upstream-shaped limitation in the same class as the already-tracked `--trust-keys`-on-
`reproductions` gap, not something to work around cockpit-side). This makes `say.go`'s existing
error branch reachable for at least the exec-level-failure case.

**Open TODO, low severity, deliberately deferred (security agent, not yet addressed):** now that
finding 8 lets real verify errors propagate instead of swallowing them, the error text reaching
Claude via `ask.go`'s `"verifying %s failed: %v"` includes the full argv `binaries.go`'s `exec()`
wraps into its error — which includes the absolute pubkey path from `cfg.TierPubkeys()`
(`RepoRoot`-joined). A broken trust config now leaks the participant repo's absolute local
filesystem path and `keys_dir` layout into the MCP tool result. Low severity — the disclosed
material is a path to a *public* key, not secret content — but a new, if narrow, disclosure
surface introduced by this diff. Worth a `[REDACTED]`-style scrub of `cfg.RepoRoot` from
verify-error text in a future pass.

**Duplication agent's other findings, not acted on in this pass (flagged for later, not blocking):**
three near-identical fake-git test harnesses in `gitops_test.go` (`TestCommitAndPush_...`,
`TestPush_...`) inline the same ~15-line setup rather than sharing a helper — notably,
`internal/binaries/verify_test.go` (added in this same change) *did* extract a shared
`runnerWithFakeBinary` helper for its 6 tests, so the pattern was already established elsewhere in
this diff, just not carried into `gitops_test.go`. Also: `Reproduces`, `Reproductions`, and the new
`isVerifyMismatch`-based `VerifyFoton`/`VerifyClaim` now each solve a variant of "is this exit code
informational or a real failure" with three different, non-unified pieces of logic in the same
file — not literal duplication, but a consistency/shared-helper opportunity worth a future look.

## End-to-end validation against a fresh kton-protocol/kton build (2026-08-12)

Before telling Michael findings 5-8 (plus the critical redaction fix and the `Reproduces` fix
found during their cold review) are actually fixed, ran a full live validation: built
`plankton`/`nekton` fresh from the current `kton-protocol/kton` source (not the stale binaries
vendored in real participant repos — confirmed `--trust-keys` on `reproductions` already exists
there, closing finding #2's upstream gap; see below), scaffolded a real throwaway private GitHub
repo (`deathbychoco/cockpit-fix-validation-20260812`) with a cockpit built from this project's
current (fixed) source, and drove the actual `Publish`/`Ask`/`Say` handlers against it end to end —
real git pushes, real signatures, two genuinely independent signers.

**All 8 scenarios passed**, each exercising a specific fix:
- A real publish, with the resulting commit confirmed actually on `origin` (finding 6).
- Denial of a signing-key path and a leading-dash path, confirming HEAD/staging untouched (finding 5).
- An identical republish staying a clean no-op (finding 7, the skip-when-in-sync path).
- `cockpit_ask query=reproductions` **succeeding** against the fresh binary + a configured trust
  tier — this is finding #2's actual point: it fails unconditionally against every vendored binary
  still in use today, since none of them have been rebuilt since `--trust-keys` landed upstream.
  Confirmed `bin/plankton`/`bin/nekton` in real participant repos (e.g. `participant-christian`)
  report the identical version string ("plankton 0.1 (reference)") as the fresh build despite
  lacking the flag entirely — a live demonstration of the still-open finding #9 (no binary
  capability/version pinning): the version string cannot be trusted to reflect capability.
- A second, genuinely independent producer (different script, byte-identical output — NOT a
  re-signed copy of the first script, which plankton's own descriptor-hash dedup would have
  silently collapsed into the same foton regardless of signer) correctly counted as ↻2 once its
  key was added to a trust tier.
- `cockpit_say` recording a real L0 reproduction claim against that second producer (findings 4 + 8
  working together with real data).
- `cockpit_ask` failing loudly, not silently, when a trust-tier pubkey path was broken (finding 8).

**One new, real bug found and fixed during this validation** (not one of Michael's original items,
not part of findings 6-8 — a distinct, third-round finding): `Publish()` makes two separate commits
(inputs+outputs, then the signed registry entry — the foton can't be authored until its permalinks,
anchored to the first commit, already exist) but discarded the second commit's sha entirely
(`if _, err := gitops.CommitAndPush(...)`) and returned the FIRST commit's sha as `CommitSHA`.
Confirmed live: `git ls-remote origin HEAD` disagreed with the reported `CommitSHA` after a real
publish. Fixed by capturing and returning the second commit's sha instead — it's genuinely the
repo's final state after `Publish` returns; the foton's own embedded `--located` permalinks
correctly stay anchored to the first commit regardless (unavoidable, and harmless since the
file bytes are identical in both commits). Regression test `TestPublish_ReturnsTheActualFinalCommitSHA`
added (`internal/tools/publish_commitsha_test.go`) — verified it fails against the pre-fix
behavior and passes against the fix, real two-commit HEAD advancement via a real local git repo
(only `push` faked, everything else — add/commit/rev-parse — runs for real).

**Confirmed, not just theoretical: the "nekton annotate --print-id gap"** already on the smaller-
items list. `Annotate()`'s doc comment claims it "returns the printed claim id", but `nekton
annotate` has no `--print-id` flag at all (unlike `plankton author`) — live validation shows
`SayOutput.ClaimID` is actually the tool's full multi-line human-readable output, not a clean id.
Not fixed here (out of scope for this pass, same class of thing as finding #2's upstream gap), but
now confirmed as a real, live-observed defect rather than a hypothetical one.

Validation harness (a temporary `_test.go` file driving the real handlers against the throwaway
repo) was removed after use — it depended on a scratch GitHub repo and freshly-built external
binaries, making it unsuitable as a permanent checked-in test (the same reasoning as the
already-tracked finding #10, machine-bound tests).
