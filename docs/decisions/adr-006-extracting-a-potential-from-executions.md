---
title: "Extracting a potential from executions that already ran"
type: "decision"
project: "kton-cockpit"
date: 2026-09-30
status: "proposed"
tags: [potentials, rays, packages, extraction, federation]
---

# ADR-006: Extracting a potential from executions

## Status

Proposed (2026-09-30), on `jam-beta`. Steps 1 to 5 are built and pass in
`acceptance/raute-zwei-backends` (`EXTRAKTION.md`, `KREUZ.md`, `REVIEW.md`):

- improvego has `ProposeRay`/`ExtractRay` and the commands `ray.propose`/`ray.extract` in two new
  sealed groups.
- The cockpit has `ask {query: "ray"}` and `publish {kind: "ray"}` (SPEC §7.6, SPEC §9.7).

What still waits for ADR-005 S5 is recording executions through `ktonpkg`'s step identity.

## Context

The usual direction is potential → realisation: a ray with holes is bound and run. The opposite
direction is at least as common in practice. Somebody ran an analysis step by step, and now it
should become something another person can run on their own data: a `kton-package/1` ray.

improvego already does this for improve workflows by hand. `LoadWorkflow` reads a workflow back,
`ParameterizeStep` turns a slot or a command-line value into a hole, and `ExportPackage` writes the
package (`cmd/diamondtest`). On the git side there is only what the registry holds: signed fotons,
connected by content hashes.

## Decision

### The idea

From a set of **realisations**, derive their **potential**. Inputs that flowed between the
executions become wiring. Inputs that came from outside become either fixed payload or a hole.
Values that differ between runs of the same code become either literals or parameters.

The executions themselves become the package's evidence. Their inputs are the **test data** of the
holes, and their outputs are the **reference members of its spectrum**. A package extracted this
way can therefore check itself immediately: realise it on its defaults, and its outputs must meet
the spectrum. Those members are outputs of recorded fotons, so the kernel does not call them
fabricated (kton §10).

### Nothing becomes a hole unless somebody names it

The extraction **proposes** and a person **chooses**. No input becomes a hole, and no command-line
value becomes a parameter, because a rule guessed it.

- **File candidates** are every input whose hash no selected execution produced. The proposal says
  which of them is the command file. It knows this as a fact of the execution: improve's
  `command-file` variable, or a token of the command line that is exactly an input's slot. The
  proposal does not judge whether something "looks like data".
- **Parameter candidates** exist only where several runs of the same step disagree at the same
  position of the command line. With a single run there are none. A number in a command line is
  not a parameter until somebody says so.
- Whatever is not chosen stays fixed: a file travels in the payload, and a value stays a literal.

### Which executions

- The selection starts at an endpoint, an output hash or a foton id, and walks the lineage
  backwards.
- **Only executions that verify against the configured trust tiers are taken.** Extraction adopts
  another party's code as fixed payload, so an unverified record must not get in.
- **Federated executions are included** when they verify. A foton from another participant counts
  as much as one's own, and its bytes must be obtainable: from the local tree, or by a locator
  whose fetch is checked against the hash.
- Several producers of the same hash are reproductions, not separate steps.

### Runs and steps

- A **run** is a connected component: executions linked by the hashes that flowed between them.
- A **step** is a group of executions across runs that share their code and their shape: the same
  command-file hash, and the same input and output slots.
- One run is chosen as the **reference**. Its values become the defaults of the holes, and its
  outputs become the spectrum.

### Where it lives

- **`ktonpkg`: `Propose` and `Extract`.** They work over backend-neutral executions (slots,
  hashes, command line, command file) plus a function that fetches bytes by hash. The rule exists
  once, like the ray identity does.
- **Adapters** turn a backend's records into those executions:
  - the cockpit's fotons: working directory, command line, repo-relative paths;
  - improvego's steps: descriptor, variables, references.
- **Cockpit, without a fourth verb:**
  - `ask {query: "ray", ref: <endpoint>}` returns the proposal. It only reads, and it contains
    verified executions only.
  - `publish {kind: "ray", from: <proposal>, choose: {…}}` writes the package, records the
    potential, and signs a `prov:wasDerivedFrom` claim from the package to the source fotons.

### One form for the command line

A package step's `protocol.command.args` has the form existing packages already use: the image as
the first line, then `<command-file>`, then arguments and `<param>` placeholders. The interpreter
does not appear; the tool supplies it, and jam-r's `tool.json` states `--entrypoint Rscript`.

The command line is part of the protocol, and the protocol is part of identity. So both adapters
write this form:

- The improve adapter reads it from the recorded descriptor.
- The cockpit adapter translates the shell line it recorded. The leading word must be the tool's
  named entrypoint, otherwise the adapter refuses; the image comes from the foton's `envRef`.

With one form, the same executions give the same package on either backend.

### Round trip, and where it depends on ADR-005

The acceptance criterion is behavioural: realise the extracted package on its defaults, and the
outputs meet its spectrum.

A stronger criterion is that a realisation of the extracted step has the **same action key** as
the original execution, because reuse would then find the original.

- **Between packages, this holds.** Extracted on either backend, the potentials have the same
  action keys as the original package under the same bindings.
- **Between a package and the executions as recorded, it does not yet hold on either backend.**
  The cockpit's `publish` records its shell line and image in the protocol; the Raute acceptance
  test already showed this for the normaliser. improve's `FotonOfStep` records `improve/step`
  without variables or params.

The fix is the same on both sides and belongs to ADR-005 S5: executors record through `ktonpkg`'s
step identity.

## What the steps found

- **Reproductions must be merged before anything is counted.** A second producer of the same bytes
  otherwise joins two runs into one, and every step appears twice. `Propose` merges executions with
  the same code, command line, and input and output bytes, and lists the ones it merged.
- **A step that passes its input through unchanged** has the same bytes as input and output. It must
  not be wired from itself. The Raute's first step does exactly this.
- **Labels that exist only inside a proposal must not reach identity.** The spectrum once named the
  reference run `run-2`, which depends on how foton ids sort. It now names the run's endpoints.
- **The order of independent steps must not depend on foton ids.** `Ray.ID` breaks ties by position
  in the array. `Propose` once ordered independent steps by foton id, and foton ids differ between
  backends, so the same computations gave two packages once the cockpit did the extracting. This
  surfaced when both reviews had to name the same package. `Propose` now orders topologically and
  breaks ties by step name. `Ray.Order` stays as it is, because changing it would move the identity
  of every existing package.
- **The same executions give the same computations on either backend.** A package extracted on git
  and one extracted on improve have the same ray id, the same potential per step, and the same
  action keys under the same bindings. A package extracted on one side produces the same bytes on
  the other.
- **improve's recorded fotons are not found by the potential's action key.** `FotonOfStep` writes
  the protocol kind `improve/step` without `variables` or `params`. A reuse lookup keyed on the
  potential's action key therefore misses runs that improve recorded itself. This belongs with
  ADR-005 S5: executors should record through the step identity of `ktonpkg`.

## Steps

| step | content |
|---|---|
| 1 | `ktonpkg.Propose` / `ktonpkg.Extract`, unit tests on synthetic executions shaped like the Raute — **done** |
| 2 | prototype in `acceptance/raute-zwei-backends`: run the Raute twice on the git side, extract with explicit choices, compare with the original package, realise the extracted package and hold it against its spectrum — **done** |
| 3 | across backends: extract on one side, install and run on the other — **done** |
| 4 | improvego on `Propose`/`Extract` instead of hand-written `ParameterizeStep` — **done** (`ProposeRay`/`ExtractRay`, starting from improve steps) |
| 5 | cockpit `ask ray` and `publish kind:ray` — **done**; recording through step identity still waits for ADR-005 S5 |
