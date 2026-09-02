---
title: "Run the published command in a pinned container, so the environment is derived rather than declared"
type: "decision"
project: "claude-science-cockpit"
date: 2026-09-02
status: "accepted"
tags: [architecture, execution, environment, reproducibility, security]
---

# ADR-003: Run the published command in a pinned container

## Status

**Accepted and implemented** (2026-09-02). Both open questions were settled by the operator; see
"Open questions, as settled" below.

## Context

`cockpit_publish` records a command. It does not run one — `plankton author` says so of itself
("RECORDS --cmd as a label; never runs it"), and the cockpit inherited that: Claude executes the
work by whatever means it has, then tells the cockpit what it ran.

Since ADR-002's successor work the cockpit also pins the execution environment into the foton, via
`plankton author --env-ref`. That value is COVERED — it rides in the descriptor into the foton id,
so two runs of the same command in different pinned environments are different fotons and a
reproduction commits to re-executing in the pinned one. It comes from `cockpit.config.json` rather
than from a tool argument, precisely because a Claude-supplied value would be an unverified
assertion baked into the record's identity.

But a configured value is still a *declaration*. The operator writes "we run in image X"; nothing
checks that the command actually ran there. The gap is small and honest today — a human set it
deliberately — and it stops being either as soon as anything drifts.

If the cockpit runs the command itself, in a digest-pinned image, then the string handed to the
container runtime and the string pinned into the foton are the same string. What ran and what is
recorded cannot differ, because there is only one value. That is the difference between an
environment pin that is *asserted* and one that is *derived* — the same move already made for the
reproduction level, which the cockpit computes rather than believes.

## Decision

*(Proposed.)*

Execution is **opt-in per repo**, off unless `cockpit.config.json` configures it:

```json
"execution": {
  "engine": "docker",
  "image":  "oci://ghcr.io/org/analysis@sha256:<digest>",
  "network": false
}
```

With `execution.image` set, `cockpit_publish`:

1. runs `cmd` inside that image, with the repo root mounted as the working directory,
2. fails outright and commits nothing if the container exits non-zero,
3. commits inputs and outputs and authors the foton as it does today,
4. pins `envRef` to the very image reference it passed the runtime.

With it unset, behaviour is exactly as today: the command is recorded, not run.

The image reference **must** carry a digest. The existing `environment.envRef` validation already
refuses an `oci://` reference without one, for the reason that applies doubly here: a tag names
whatever it points at today, so running "in that image" would pin nothing.

If both `execution.image` and `environment.envRef` are configured and they disagree, loading the
config fails. A repo that says it runs in A while recording B is not a configuration, it is a
false record waiting to be signed.

### Why this does not breach "Nicht bauen"

- **No fourth verb.** Execution happens inside `cockpit_publish`, which already takes `cmd`. The
  tool surface Claude sees does not grow.
- **No reimplemented kernel logic.** Canonicalisation, hashing, signing and verification stay with
  the binaries. The cockpit gains one more thing it shells out to.
- **Still deletable.** With `bin/cockpit` removed, the same operation remains runnable by hand:
  `docker run --rm --network none -v "$PWD":/work -w /work <image@sha256:…> sh -c "<cmd>"`, then
  `plankton author … --env-ref oci://<image@sha256:…> --cmd "<cmd>"`. Nothing here can only be done
  through the cockpit.

### What genuinely changes, and it is not small

The cockpit becomes able to **execute**, not only record. In the Claude Code CLI that adds little —
Claude has Bash there anyway. In claude-science it adds a great deal: the cockpit is the *entire*
surface Claude has, deliberately three verbs and nothing else, and this puts arbitrary command
execution behind one of them. The container is the boundary that keeps that acceptable, which is
why its constraints are part of the decision and not a detail:

- no network (`--network none`) by default — a build that reaches the internet is not reproducible,
  and it is also the cheapest exfiltration path out of the sandbox;
- the repo mounted, and nothing else;
- non-root, mapped to the invoking uid/gid, so outputs are not root-owned artefacts the operator
  cannot clean up.

## Open questions, as settled

1. **Verification: against a real engine.** Docker was started rather than stubbed. Six tests drive
   an actual runtime, behind a `docker` build tag so the default suite keeps its no-skips property:
   the command runs and produces its output, a non-zero exit publishes nothing and leaves no commit,
   a declared output the run never produced is refused, files come back owned by the invoking user,
   and `envRef` is the same string the runtime was given.

   The isolation test carries a negative control, without which it proves nothing: the same command
   under `network: true` must reach out. A container with no DNS configured at all would otherwise
   report "isolated" while `--network none` did nothing, and the test would pass for the wrong
   reason.

2. **Network: isolated by default, configurable.** `--network none` unless the repo opts out.
   `cockpit_publish` reports `networkAllowed` when it did, so whoever reads the result sees that the
   run depended on something the foton does not pin, rather than having to know the repo's config.

## claude-science: the platform states the container, the cockpit does not guess

claude-science starts containers for execution, using images the operator brings. The work is
already containerised there in an image someone chose and pinned. Spawning another container from
inside the cockpit would add a layer and change nothing about what the foton can honestly say, so
`execution.image` is not the answer there.

Neither is configuring the same digest a second time in `cockpit.config.json`. That would mean the
operator maintains one value in two places, and if they drift — claude-science gets a new image,
the cockpit's config does not — every foton published afterwards pins an image that did not run.
Nothing fails. The value is COVERED, so it becomes part of records that are correctly signed and
wrong, and a reproduction "via" one of them commits to re-executing where the original never ran.
No check on this side can catch it: the cockpit runs as a Local command connector, a host process
outside the container, and never sees the run.

A declaration that cannot be checked, duplicated across two systems, is worse than no pin at all —
an unpinned foton is honestly unpinned, while a stale one asserts something false. This project's
own rule covers the case: a capability gap is raised where it belongs, not worked around here.

**So: claude-science should state which container it executed in.** One value, from the side that
knows it, passed to the connector — an injected environment variable is enough. The cockpit then
records what it is told rather than what it was configured with, and can refuse a mismatch instead
of silently preferring one. See "Requirements on the platform" below.

Until that exists, the cockpit does not pretend. `environment.envRef` stays for environments it can
neither run nor be told about — a nix store path, a run-server id — and `doctor` says in as many
words that such a pin is asserted rather than observed.

## Requirements on the platform (claude-science)

**P1 — report the executing container to the MCP connector.**

The connector process runs on the host, outside the container claude-science executes in, and has
no way to learn its image. Exposing it — `CLAUDE_SCIENCE_EXEC_IMAGE=oci://…@sha256:…` or equivalent,
in the connector's environment — turns the cockpit's environment pin from a duplicated declaration
into a value derived from the run.

The digest matters, not a tag: a tag names whatever it points at today, so a reproduction
committing to "that image" would commit to nothing.

With it, the cockpit can do what it already does elsewhere — record what it observed, and refuse
when an observation and a configuration disagree — instead of asking an operator to keep two
systems in sync by hand.

## Consequences

- The environment pin stops being a declaration and becomes a property of how the record was made.
- Repos that do not configure `execution` are entirely unaffected.
- The cockpit acquires a dependency on a container runtime, but only for repos that opt in.
- `doctor` gains an obligation: report the configured engine and image, and refuse an image
  reference without a digest.
