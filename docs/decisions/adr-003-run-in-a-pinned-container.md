---
title: "Run the published command in a pinned container, so the environment is derived rather than declared"
type: "decision"
project: "kton-cockpit"
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
("RECORDS --cmd as a label; never runs it"), and the cockpit inherited that: the caller executes the
work by whatever means it has, then tells the cockpit what it ran.

Since ADR-002's successor work the cockpit also pins the execution environment into the foton, via
`plankton author --env-ref`. That value is COVERED — it rides in the descriptor into the foton id,
so two runs of the same command in different pinned environments are different fotons and a
reproduction commits to re-executing in the pinned one. It comes from `cockpit.config.json` rather
than from a tool argument, precisely because a caller-supplied value would be an unverified
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
  tool surface a session sees does not grow.
- **No reimplemented kernel logic.** Canonicalisation, hashing, signing and verification stay with
  the binaries. The cockpit gains one more thing it shells out to.
- **Still deletable.** With `bin/cockpit` removed, the same operation remains runnable by hand:
  `docker run --rm --network none -v "$PWD":/work -w /work <image@sha256:…> sh -c "<cmd>"`, then
  `plankton author … --env-ref oci://<image@sha256:…> --cmd "<cmd>"`. Nothing here can only be done
  through the cockpit.

### What genuinely changes, and it is not small

The cockpit becomes able to **execute**, not only record. At an ordinary shell that adds little — the caller has one anyway. In a sandboxed agent host it
adds a great deal: the cockpit is the *entire* surface a session has, deliberately three verbs and nothing else, and this puts arbitrary command
execution behind one of them. The container is the boundary that keeps that acceptable, which is
why its constraints are part of the decision and not a detail:

- no network (`--network none`) by default — a build that reaches the internet is not reproducible,
  and it is also the cheapest exfiltration path out of the sandbox;
- the repo mounted, and nothing else;
- non-root, mapped to the invoking uid/gid, so outputs are not root-owned artefacts the operator
  cannot clean up;
- `--cap-drop=ALL --security-opt=no-new-privileges`: reading and writing files in a working
  directory needs no capabilities, and nothing should become more privileged than the command;
- **the trust base emptied out of the mount** — see below;
- what the run prints back is capped, since it reaches the model.

### The mount contains the trust base, so the trust base is emptied

The constraint "the repo mounted, and nothing else" was not enough, because the repo *is* where the
trust base lives: `keys_dir`, `bin_dir`, the registry, `.git` and `cockpit.config.json` are all under
`RepoRoot`, and the defaults put them there.

The ordering makes it concrete. `container.Run` happens first; then `CommitAndPush`, which fires
`.git/hooks` **on the host**; then `Author`, which execs `bin/plankton` **on the host**, from the
mount.

And the failure is not adversarial, which is what makes it worth closing rather than forbidding:
`go build -o bin/plankton ./reference/cmd/plankton` is a legitimate command straight out of this
project's own instructions. Run in the container against an unmasked mount, it replaces the binary,
and the record is then authored by a kernel `doctor` never checked. Nothing fails — what ran and what
was recorded simply diverge.

So the room is emptied rather than the command constrained: fresh tmpfs over `keys/`, `bin/`, the two
registry directories and `.git`, and an empty read-only bind over `cockpit.config.json`. A denylist
of forbidden commands is a guessing game; an empty directory is not.

The registry is included although the finding did not ask for it: a command that can write into
`objects/` can plant records the cockpit later reads back as its own — the same silent divergence,
one layer down — and nothing a published command legitimately does needs to reach it.

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

## Naming the environment vs. running it

The two are alternatives, not a hierarchy, and a repo may use either.

**Naming it.** `cockpit_publish` takes an optional `envRef`: the caller says which container the
work ran in, and the cockpit records that. This is the normal case, and the only one that can work
when a session moves between many containers — as a sandboxed agent host does, starting containers from
images the operator brings. No configured value could be right for all of them, so the value belongs
in the call.

That it is a claim is not a weakness to be engineered around. The inputs, the outputs and the
command are already claims: the caller names the paths, and the cockpit hashes the files it is pointed
at. An omitted input would be a more consequential false statement than a wrong `envRef`, and
nothing prevents that either. A foton is a signed statement about what someone did; the hashes make
inputs and outputs checkable, the signature makes the signer accountable for the rest. plankton says
the same of the command: it RECORDS it, and never runs it.

**Running it.** With `execution.image` configured, the cockpit starts the container itself. The
environment then stops being a claim and becomes an observation — the string handed to the runtime
and the string pinned into the foton are the same string. This is for a session with a shell in an
environment nobody pinned, such as an ordinary shell on a laptop.

The one thing the cockpit will not do is record an environment other than the one it ran in: with
execution configured, an `envRef` naming something else is refused rather than preferred either way.

`environment.envRef` in the config remains as a default for repos whose environment nobody names
per call. A supplied value wins over it.

## Consequences

- The environment pin stops being a declaration and becomes a property of how the record was made.
- Repos that do not configure `execution` are entirely unaffected.
- The cockpit acquires a dependency on a container runtime, but only for repos that opt in.
- `doctor` gains an obligation: report the configured engine and image, and refuse an image
  reference without a digest.
