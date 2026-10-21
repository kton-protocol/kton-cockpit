---
title: "The verb count is not fixed"
type: "decision"
project: "kton-cockpit"
date: 2026-10-16
status: "accepted"
tags: [architecture, verbs, mcp, recorder]
---

# ADR-009: The verb count is not fixed

## Status

Accepted (2026-10-16). Supersedes the "no fourth verb" item of SPEC §13 and the
places that repeated it (AGENTS.md, CONTRIBUTING.md, README.md, ADR-003 "No fourth verb", ADR-005
section 5, ADR-006, ADR-008). Those ADRs stay as written: they record why their own features fit into three
verbs, which is still true of them.

## Context

The cockpit has three verbs, `publish`, `say` and `ask`, and SPEC §13 forbade a fourth. The rule
kept a session's surface small and made every new capability argue its way into one of the three.

The recorder (`recorder/`) observes work done outside the cockpit, in an R session, a Quarto
document or a NONMEM run, and has to hand what it saw to kton. Forcing every recorder operation
through the existing three bends their meaning: "persist this state" is not a publish of one
computation, and an annotation of an observed run is not a claim from a template the operator
chose ahead of time. The project owner decided the fixed count goes.

## Decision

- **A new verb may be added** when an operation does not fit an existing one without changing what
  that verb means.
- What the "no fourth verb" rule protected stays, now as rules of its own:
  - **One implementation per verb**, reached the same way from the command line, MCP and the Go
    library. A guard belongs to the configuration, never to the transport.
  - **The configuration is the ceiling for every verb.** `verbs` in `cockpit.config.json` names
    which are active; a session cannot enable a disabled one. A new verb is off unless configured.
  - **Every verb is specified** in SPEC with the tests that check it, like the first three.
  - **Deletable** (SPEC §13) still holds: whatever a new verb does must stay performable by hand
    with the substrate.
- No new verb is added by this decision. Which recorder operations become verbs is decided with the
  recorder's design (plan section A).

## Consequences

- Statements that the cockpit "has exactly three verbs" stay accurate until a fourth exists; they
  describe the current surface, not a rule.
- Adding a verb is now a design question per verb, not a rule violation. The cost that the old rule
  avoided, a growing surface a session can reach, is carried by the configuration ceiling instead.
