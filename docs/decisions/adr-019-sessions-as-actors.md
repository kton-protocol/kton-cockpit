---
title: "Sessions as actors: a session's claims, its key at the MCP door, a guard module that checks and records"
type: "decision"
project: "kton-cockpit"
date: 2026-11-21
status: "proposed"
tags: [architecture, module, session, access, mcp, serve, qm]
---

# ADR-019: Sessions as actors

## Status

Proposed 2026-11-21. Requested by qm (`a80d6c84`) on MH's concept of 20./21.11.; MH answered the seven open
questions on 21.11. (relayed by qm, `966fc6ad`), and those answers are the decisions below. The claim names
were agreed with the cockpit lane (area qms) and fixed by qm's tie-break (`08aec228`). The concept document is
on ai1; this ADR follows qm's summary of it.

## Context

People and agents sign today; the sessions an agent runs in do not appear. MH wants access to data decided by
what a session is: which model, hosted where, with or without network, on whose behalf. The server that holds
the data enforces it and records every access, never the model. The pilot: data class `customer-information`,
action `read`, allowed only for internally hosted models without network.

SPEC §13 keeps the core free of trust logic beyond applying the configuration, and of cockpit-owned mutable
state. MH's answer to Q1: the check and its record are a **module**, through the ADR-015 module door; the core
is not changed beyond the door.

## Decision

### 1 A session is a key with two claims about it

A session has its own key. The shell makes it, for a limited lifetime (Q4). Two claims about the session's
key, each signed by who knows what it says, never by the session itself:

| template | signed by | fields |
|---|---|---|
| `session-environment` | the machine's key (one per host, Q6) | `model`, `modelFiles` (one ref to a manifest of the weight files' hashes), `modelHosting` internal\|external, `network` none\|restricted\|open, `machine`, `evidence` (one ref: the run-in-container record that shows the isolation), `validUntil` |
| `session-behalf` | the person (or lane) it acts for | `onBehalfOf`, `purpose` (optional), `validUntil` |

A `session-ended` claim, by either signer, ends a session before `validUntil` (Q4). Predicates are under
`https://kton.dev/v/session/`. Machine keys are a trust tier of their own (`machines`): a machine's word
counts only where the configuration trusts that tier, as for any signer. For the pilot, `network: none` is
shown by a run-in-container record with `--network none` that the cockpit observed (Q6).

### 2 Data classes are claims on scopes

A scope's data class is a `classifiedAs {class}` claim on its seed (Q2), by a key the configuration trusts
for it. A request reads or writes the classes of the scopes it touches; a scope with no class claim has none.

### 3 The key at the door

`cockpit mcp` and `cockpit serve` (Q7: these first; the recorder's MCP later) take the session: `initialize`
names its key (`session: {keyid}`), the server hands out a nonce, and the session signs it, so the connection
holds the key rather than names it. Every request on the connection then acts as that session. A connection
without a session acts as today.

### 4 The guard: a module

ADR-015's module door gains one optional field:

```go
Guard func(ctx context.Context, r Request) Decision   // nil: the module guards nothing
```

`Request` is the session (its key and its verified claims), the action (`read`, `write`, `sign`), the
resources (scope seeds, tool names) and their data classes. `Decision` is allowed, or refused with the rule and
the missing attribute. The core calls every registered guard before it answers a request at the door, and
answers nothing a guard refuses. Without a guard module in the build, nothing changes.

The `session` module is that guard. It asks the rules (`qm.rules`, a package's named query
`access-allowed($sessionKey, $dataClass, $action) -> ?allowed ?rule ?why`) and decides. A denied read is
refused with its reason, `access.denied` naming the rule and the missing attribute, not hidden (Q3).

### 5 The record: a local log, hash-chained, signed daily

Every decision, allowed or refused, is a line in the module's access log under `.kton/local/access/`: `{at,
session, dataClass, action, resource, decision, rule, environmentClaim, behalfClaim, prev}`, where `prev` is
the hash of the line before (Q5). Once a day the module signs the day's last hash as a claim (the server's
key), so a changed or dropped line shows. No claim per access. The log is the module's state, not the core's
(SPEC §13).

### 6 Lanes are sessions under the same rule

MH's addition: a lane may only run on a model cleared for the highest data class its folder, repositories,
scopes and MCP servers reach. That is checked where a lane starts, by the lane runner (`kw`, kton-coordination),
against the same rules. The module gives it one operator command, `cockpit session check --lane <folder>
--model <environment claim>`: it collects the data classes the lane reaches (its repositories' configurations,
their scopes' class claims, the MCP servers the folder configures) and answers whether the rules allow the
model, with the rule and what is missing. `kw` refuses to start a lane the check refuses.

### 7 Out of scope

MCP servers outside kton (improve-api, Atlassian); enforcement inside a model; who may classify a scope
(a matter of the trust tiers, as for any claim).

## Consequences

- One door change (the `Guard` field) and one module; SPEC §13 stays as it is.
- A session's attributes are signed facts, by the machine and by the person, so a rule never reads a model's
  name for its hosting.
- Every access is accountable by session key, and the log cannot be edited silently after its daily seal.
- Lanes and sessions share one rule set.

## Tests (before the module lands)

A session without its environment claim; with `network: open`; with an environment claim from an untrusted
machine key; with an expired or ended behalf claim; a replayed nonce; a scope without a class (no rule
applies); the pilot rule end to end (allowed and refused, both in the log); a line removed from the log
after its day was sealed (the seal no longer matches); `session check --lane` refusing a lane that reaches
customer information with an external model.

## Alternatives

- **The check in the core** (amending SPEC §13): rejected by MH (Q1).
- **One claim per access:** rejected by MH (Q5): the registry would grow with every query.
- **Hiding a denied read** ("not visible = does not exist"): rejected by MH for this case (Q3): a refusal with
  its reason, recorded.
