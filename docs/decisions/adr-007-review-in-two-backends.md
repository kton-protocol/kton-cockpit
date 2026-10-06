---
title: "Review: improve decides in improve, git decides in nekton"
type: "decision"
project: "kton-cockpit"
date: 2026-09-30
status: "proposed"
tags: [review, four-eyes, improve, nekton, federation, projection]
---

# ADR-007: Review in two backends

## Status

Proposed (2026-09-30), on `jam-beta`.

## Context

A review is a person's judgement on something, such as a package or a qualification run: accepted
or rejected, with the finding. It matters for the same reason as everything here. Somebody else
must be able to check later who judged what, about which exact bytes.

Both backends can hold a review, but they hold it differently.

- **improve has a review process.** A review with entries and invited reviewers, four-eyes
  enforced by the server (the requestor cannot review their own entry), and an audit trail.
- **git has nekton.** A signed claim is the only durable judgement there is.

## Decision

**Where the review is decided is the authority. Anything else is a projection of it.**

| | decided in | the nekton claim is | signed by |
|---|---|---|---|
| improve | the improve review (entries, reviewer, four-eyes, audit trail) | a **projection**: what improve recorded, derived from a decided entry, never written by hand | improve's system key; it attests the entry, not the person |
| git | nekton, and only nekton | **the review itself** | the reviewer's own key |

Consequences:

- **In improve a projected claim cannot outrank improve.** A projection is derived from a decided
  entry (improvego `ReviewClaim`). An entry that is not decided has no projection, and a projection
  whose entry was later withdrawn is stale. The answer is to ask improve.
- **In git, four-eyes is a matter of keys and tiers, not of cockpit logic.** The reviewer is another
  participant, with their own key, and their claim reaches this repository through the federation.
  The configuration puts that key into a tier ("pruefer"), and `ask about` shows the review with the
  tier it verified into. A review signed with the author's own key verifies into the author's own
  tier and nowhere else, so a filter on the reviewers' tier leaves it out. The cockpit adds no rule
  of its own (SPEC §13).
- **Both sides use one predicate:** `https://scinteco.com/ns/qualification/v1#reviewed`, with the
  outcome `accepted` or `rejected` and the finding. In git that is the template `reviewed`; in
  improve it is the projection improvego already writes.
- **Both sides review the same subject.** ADR-006 showed that the same executions yield the same
  package on either backend, down to its bundle id. A review names that bundle id, so the git review
  and the improve review are judgements on the same bytes and can be compared, and collected side by
  side in a federation.

## Checked in

`acceptance/raute-zwei-backends` reviews the extracted Raute package on both sides. In git the
reviewer is a second participant, and its claim is carried into the author's registry. In improve
the reviewer is a second improve user, and the projection is carried into the git registry, where
it verifies against a tier holding improve's system key. The result is in `REVIEW.md`.
