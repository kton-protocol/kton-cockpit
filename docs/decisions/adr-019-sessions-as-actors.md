---
title: "Sessions as actors: a session's claims, its key at the MCP door, a guard module that checks and records"
type: "decision"
project: "kton-cockpit"
date: 2026-11-21
status: "accepted"
tags: [architecture, module, session, access, mcp, serve, qm]
---

# ADR-019: Sessions as actors

## Status

Accepted by MH (2026-11-23), with section 8 answering userinterface's review. Proposed 2026-11-21. Requested by qm (`a80d6c84`) on MH's concept of 20./21.11.; MH answered the seven open
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

### 8 The shell's side (userinterface's review on #219)

**Who signs the environment.** The shell makes the session key (Q4) and keeps its private half. It asks the
cockpit on the host where the model runs to sign `session-environment`: serve's `session/environment
{session, model, modelFiles?, modelHosting, network, evidence?}`, CLI `cockpit session environment …`. That
cockpit holds the host's machine key at its machine path (ADR-016, never in a repository or an image) and
signs only what it can back: `modelHosting: internal` with `network: none` needs the `evidence` record, which
it checks; `external` or `open` needs none, since it clears nothing more. A window whose model is an external
API (a desktop VS Code window with a hosted model) gets its claim from its own host's cockpit, saying
`external`. A code-server container holds no machine key; the host's cockpit signs for it.

**The behalf claim is the person's.** `session-behalf` is signed with the person's key, through the shell's
sign dialog, once per session, never with an agent key. There is no renewal approved in advance: the person
chooses `validUntil` in that dialog, and when it runs out the shell asks again (a new behalf claim). The shell
warns before it runs out.

**The session does not replace `as`.** The session is the actor; `as` still names the signing key. A `say`
in a session signs with the key it names (person key for decisions, agent key for records, #196), and the
guard is asked for the action `sign` with that key as the resource, so the rules may limit which keys a
session uses. One window holds one session and both keys.

**The CLI form.** The command line has no `initialize`, so each call carries its session: `--session
<keyid>` (or `KTON_SESSION`) with `KTON_SESSION_SIG`, the session key's signature over the call (its
arguments, working directory, time and a random nonce, canonical JSON). The module refuses a signature older
than 60 seconds and a nonce it has seen (its local state). No token is held between calls.

**Required sessions.** With the module in the build, `session.required: true` in the configuration refuses
every request without a session, at the door and on the command line; otherwise a request without one acts
as today. Without it a CLI window would stand outside the guard.

**Refused versus not visible.** The guard judges a session's clearance for a data class, and naming a class,
its rule and the missing attribute reveals nothing: classes are vocabulary. ADR-018's visibility comes first,
and a scope a reader cannot see is never asked about. A request that names a scope it may not read is refused
with `access.denied`, its reason given (the requester knew the name). A query over the union is answered over
the scopes the session is cleared for, and says which classes were left out, never which scopes. The shell
shows it as "this session is not cleared for <class> (rule …, missing …)", never as "hidden data exists".

**Status and the log.** `session/status` (CLI `cockpit session status --json`) returns the connection's
session: the environment claim's model, hosting and network, on whose behalf, until when, and whether it has
ended or expired. A request in an ended or expired session is refused with `session.ended` or
`session.expired`, so the shell disables acts with that reason. `session/log {since}` returns the access log's
lines of the connection's own session; the operator command `cockpit session log --on-behalf-of <key>`
returns those of every session on that person's behalf, for "what was refused today and why".

### 9 For the module's build (kton-review on the v0.5.11 release)

**The evidence is bound to the session (E1).** The `evidence` of `network: none` is the run-in-container
record that made the session's key or names it; the machine key refuses `internal`/`none` with any other,
so an old `--network none` run vouches for no session but its own.

**The most restrictive class wins (E2).** A scope with several `classifiedAs` claims (by trusted keys) has
every class they name, and a request needs clearance for each; never the latest alone. `doctor` names the
unclassified scopes beside the classified ones, so an open scope is seen, not assumed.

**What carries no class (E3), open for MH.** Steps, unscoped claims and loaded files' graphs are in no scope
and so carry no class: a union query answers over them whatever the session's clearance. Proposed: a
loaded file is classified as a scope is (`classifiedAs` about its hash), and `session.unclassified: open |
closed` in the configuration decides the rest, `open` by default as section 2 decides for scopes; with
`closed`, a session below a configured class reads no unclassified graph.

**No unsealed lines (E4).** Lines written since the last seal could be dropped undetected until the day's
seal. The module also seals at shutdown and every N lines (configurable, 1000 by default); a day whose
last line is not sealed is a finding at the next start and in `doctor`.

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
customer information with an external model; a machine key refusing to sign `internal`/`none` without its
evidence; a `say` in a session signing with a key the rules do not allow it; a CLI call with a stale or
replayed signature; `session.required` refusing a call without a session; a union query leaving out a class
and naming the class, not the scope; `session/log` returning only the connection's own session; a machine key refusing `internal`/`none` with another session's evidence (E1); a scope classified
twice answering with both classes (E2); `doctor` naming unclassified scopes; with `session.unclassified:
closed`, a union query leaving out steps, unscoped claims and unclassified loaded files for a session below a
class (E3); a line dropped after the last seal and before shutdown, found at the next start (E4).

## Alternatives

- **The check in the core** (amending SPEC §13): rejected by MH (Q1).
- **One claim per access:** rejected by MH (Q5): the registry would grow with every query.
- **Hiding a denied read** ("not visible = does not exist"): rejected by MH for this case (Q3): a refusal with
  its reason, recorded.
