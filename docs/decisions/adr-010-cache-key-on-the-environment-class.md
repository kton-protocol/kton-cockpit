---
title: "Reuse is keyed on the environment class, not on the concrete environment"
type: "decision"
project: "kton-cockpit"
date: 2026-10-16
status: "proposed"
tags: [architecture, environment, reproducibility, qualification, reuse, action-key]
---

# ADR-010: Reuse is keyed on the environment class, not on the concrete environment

## Status

Proposed (2026-10-16; aligned with the kernel on 2026-10-20). The direction was decided by the project
owner. It needed a kton change first, because it touches the action key: kton-protocol/kton#211, merged as
#223 (`5a8895d`, kton 0.3.0-dev, `foton/v1` only). The cockpit side is implemented once the cockpit
builds against kton 0.3.0. From then on this ADR supersedes the part of ADR-003 that makes `envRef`
COVERED ("pins `envRef`…", and the `Environment` comment in `internal/config/config.go`) **for records
that declare a class**. Everything else in ADR-003 stands: running in a digest-pinned image, the mount
rules, deriving rather than declaring the environment, and a covered `envRef` when no class is declared.

## Context

Pharmacometric tools call tools. PsN, pharmpy and bbr (through bbi) each have their own logic,
such as resampling, stepwise covariate search, VPC statistics and result parsing, and they call
NONMEM for every estimation. Qualifying them as a whole qualifies a closure: PsN version × Perl ×
NONMEM × compiler, or pharmpy × Python × NONMEM, or bbr × R × bbi × NONMEM. Every combination would
need its own spectrum, each with estimation tolerances.

Seen as functions, the closure decomposes. An orchestrator `f` calls the engine `g` at a process
boundary, and the boundary is the same for all three tools: `nmfe`. Then:

1. `g` (NONMEM) is qualified once, by its spectrum with a normalizer and tolerances (kton §10).
2. `f` is qualified against `g`'s interface. During `f`'s qualification the `nmfe` calls are served
   by a stub that looks up the call's action key in kton and hands back the outputs of a reference
   foton. This is the pharmpy shim the project already has, generalised. `f` then runs without NONMEM
   in its image and, with its seed as an argument, deterministically.
3. `f∘g` is qualified when `f` is qualified against the interface and `g` comes from a qualified
   class. No combination is qualified on its own.

In production the same stub keeps images lean: an R image has R and stubs for `nmfe`, `execute`,
`bbi` and `python`. A call whose action key kton already knows is not run again; one it does not
know is dispatched to the engine's own image (in improve) and published, so the next call is a hit.

This only works if a cache hit can cross concrete environments of the same class. Today it cannot:

- kton §6.5 already separates the two. `protocol.descriptor.environment` is a `SpectrumId`, the
  class, and is COVERED. Concrete env-data (OCI digest, nix, renv) is CARRIED and "MUST NOT affect
  any identity".
- The reference implementation does otherwise. `plankton author --env-ref` writes the concrete
  reference into the descriptor (`reference/cmd/plankton/author.go`), so it lands in `protocol.ref`,
  the action key and the foton id.
- The cockpit relies on that on purpose (ADR-003; `Environment` in `internal/config/config.go`:
  "Both fields are COVERED").

As a result, two qualified NONMEM installations of the same class give the same call two different
action keys, and nothing can be reused between them.

## Decision

**When a class is declared, the action key, and with it reuse, depends on the environment class (the
env-spectrum), not on the concrete environment.**

1. **`envRef` is carried when a class is declared.** With `environment` configured, the exact image
   digest, nix path or run-server id travels as signed verification material beside the `foton/v1`
   record (scheme `kton-env/v1`, one statement per producer, signed by the producer's key), so it is
   tamper-evident and still says where the work ran. It no longer moves the action key or the foton id.
   A consumer reads the env statement signed by the same key whose foton signature it trusts. This is
   the reading kton §6.5 gives; kton#211 makes the reference implementation follow it.
2. **Derived, not declared, still holds.** With `execution.image` configured, the cockpit records the
   very string it handed to the runtime, exactly as ADR-003 decided. Only the meaning changes, from
   "part of the identity" to "evidence of where it ran".
3. **No class, nothing changes.** Without `environment`, `envRef` stays covered, as today: the
   digest is then the only statement about what ran. Two concrete environments give two action keys,
   so there is no reuse across environments, and no consumer rule is needed for it. The same holds for
   every `foton/v0` record, which has no place for carried material.
4. **Reuse needs accepted qualification.** A class is only a label until it is qualified: anyone can
   name a class and run in an image that never fulfilled it. A hit across environments counts only if
   the hit's carried `envRef` has a `qualifies-as` claim for the class, signed under a trust tier the
   consumer configured, the same rule `cockpit ask` already applies to records (a consumer rule,
   kton §8). `Registry.Reuse` keeps returning every candidate
   for an action key; the decision belongs to the consumer, not to the kernel.
5. **Reproduction via a foton** means "in some environment of this class", not "in this image", when
   a class is present; without one it still means this exact environment. Differences within the
   spectrum's tolerance are what the qualification has already accepted.

### The argument for covering `envRef`, and why it no longer applies

`author.go` gives the reason it is covered: a normalizer or program is only trustworthy if the
environment it ran in is pinned, not a mutable path. That holds when there is no qualification, since
then the image is the only thing that says what ran. With a class there is a stronger statement: the
environment fulfilled a spectrum, and someone accountable signed that it qualifies. A normalizer that
runs in a qualified class is trustworthy because of that claim, not because of a digest. Where no
class exists, rule 3 keeps the old guarantee unchanged: the digest stays covered.

## Consequences

- One NONMEM qualification serves every orchestrator. PsN, pharmpy and bbr are each qualified against
  the `nmfe` interface, never in combination.
- Results are reused across machines, images and sites whose environments qualify for the same class.
  That is the point of qualifying them.
- Fotons that already carry `envRef` in their descriptor keep their identity; they were authored
  under the old rule and stay valid (monotonicity, kton §11). New fotons of the same computation
  written with a class get a different id and action key. The cockpit's test vectors and any reuse by action key across the change see two
  records for one computation.
- The cockpit's part, once it builds against kton 0.3.0: with `environment` set, pass both to `author`
  and attach the `kton-env/v1` material the kernel produces; `ask` and reuse take the env statement
  signed by the trusted producer key; tests for both cases (the kernel's own regression tests, kton#211
  item 7, cover the action key).
- An orchestrator's spectrum needs the reference fotons of every `nmfe` call it makes. The recorder
  can supply them: it sees each call, the files it read and the files it wrote.
- Open, and not decided here:
  - how generated inputs (PsN's rewritten `psn.mod`, its directories) get stable action keys, since
    an input normalizer before the key becomes part of the identity and must itself be a qualified
    potential;
  - how adaptive orchestration (SCM, bootstrap with stopping rules) is checked for path stability
    when a qualified engine differs within tolerance;
  - which hidden inputs (`psn.conf`, `bbi.yaml`, pharmpy configuration, NONMEM's sizes) belong to an
    orchestrator's environment class.
