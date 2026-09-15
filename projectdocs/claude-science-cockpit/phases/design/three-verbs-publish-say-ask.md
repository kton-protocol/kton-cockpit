---
title: "The three verbs in detail: publish, say, ask"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [mcp, tools, plankton, nekton]
---

> **Design phase, 2026-07-22.** This records what was designed and why, at that time. The shipped
> contract is [`spec/SPEC.md`](../../../../spec/SPEC.md), whose every normative clause names the test
> that checks it; where the two disagree, the spec is right and this is history. Kept because the
> reasoning behind a decision does not survive in the clause that resulted from it.

# The three verbs in detail: publish, say, ask

## Summary

Grounded in the actual CLI usage blocks read from `reference/cmd/plankton/main.go` and
`nekton/reference/cmd/nekton/main.go`, and in the manual recipe documented in the participant
template's `CLAUDE.md` (today's hand-run version of exactly what these tools automate).

## Details

### `cockpit_publish` (veröffentlichen)

Input: `{ inputs: [path...], outputs: [path...], cmd: string, corpus?: [claimOrFotonRef...] }`

> **Superseded.** `publish` also takes `envRef`, the exact execution environment pinned into the
> record (§7.5), and the cockpit may run the command rather than record it (ADR-003, §10).

1. `git add <inputs> <outputs> && git commit && git push` — commit **first**, so a commit-pinned
   permalink can be built (mirrors the manual recipe's ordering exactly: a foton's locators must
   be fetchable, which needs the commit sha to exist already).
2. Build `--located <path>=https://raw.githubusercontent.com/<owner>/<repo>/<sha>/<path>`
   automatically for every input/output — Claude supplies plain paths, never constructs the URL.
3. `plankton author --in ... --located ... --out ... --located ... --cmd <cmd> --sign
   <identity.plankton_key> --add --print-id`.
   If `corpus` is supplied (nekton claim refs that Claude's own reasoning drew on as a basis), pass
   them as additional `--in` refs. This satisfies the spec's "Lake bleibt Lake" rule: if reasoning
   over the graph becomes the basis for new work, that reasoning must be registered as a foton
   whose inputs are the touched nektons — never a silent re-attestation of processed claims as if
   they were fresh data.
4. `git add registry && git commit && git push`.
5. Return a structured result — foton id, output hash(es) via `plankton hash`, commit sha,
   permalinks — never raw CLI stdout.

### `cockpit_say` (sagen)

Input: `{ subject: fotonIdOrHash, template: string, fields: object }`

1. Reject if `template` is not in `claims.allowedTemplates`. Field names/types are validated
   against `nekton templates --show <name>` — Claude can't invent a claim shape.
2. Special case for `template === "reproduces"`: the cockpit, not Claude, determines the `level`
   and `reproducedBy` fields. It runs the reproduction precondition itself, per
   `reproduction.requiredLevel`/`normalizer` in config — Claude supplies which foton it reproduced
   and its own resulting output, never a self-declared L0/L1/L2 level.
3. `nekton annotate <subject> --template <template> --set k=v... --sign <identity.nekton_key>
   --add`.
4. `git add registry && git commit && git push`.
5. Confirm registration with `nekton about <subject>` before returning — mirrors the manual
   workflow's explicit "CONFIRM it registered" step — and return the claim id plus that
   confirmation.

### `cockpit_ask` (fragen)

Input: `{ query: "producer"|"uses"|"lineage"|"reproductions"|"about"|"by", ref: string, filter?: {
trustTier? } }`

(Originally designed with a richer filter — `minRepro`/`level`/`signer`/`scope` in addition to
`trustTier` — but only `trustTier` shipped in v1; the rest remain a possible future extension, not
yet implemented in `internal/tools/ask.go`.)

> **Superseded.** All five dimensions are implemented, and an unknown tier, level or signer is
> refused rather than silently matching nothing — see [`spec/SPEC.md` §9.2](../../../../spec/SPEC.md).
> `ask` also takes a required `axis` for the `by` query, which this entry does not show; without it
> the query never worked, because `nekton by` takes two arguments and was being given one.

1. Run the matching read-only command against the local registry (`plankton
   producer/uses/lineage/reproductions`, `nekton about/by`), and/or the aggregator's mirrored
   `mirror/union.json` (or `kton mirror`/`serve` for a live query — see the open question in the
   [rollout entry](rollout-and-verification-plan.md)).
2. **Verify, don't declare** — for every record returned, call `plankton verify <id> <pubkey>` /
   `nekton verify` against each pubkey in the resolved trust tier. Never trust the envelope's
   declared `keyid`/`by` field (both binaries already print an explicit warning when a declared
   keyid doesn't match the actual verifying key — this is existing, not new, behavior). Resolve
   each record's tier from the *verifying* key, before any filter is applied.
3. Apply Claude's requested `filter` — it can only **narrow** within what `trust.tiers` already
   resolved; it can never surface a tier or signer absent from the static config.
4. The response always states which filter was actually applied, explicitly, including the case
   of no filter — an active filter must travel with the answer, and "no filter" is a stated fact,
   not a silent default.
5. Return structured, already-verified data only — never a raw unverified envelope.
